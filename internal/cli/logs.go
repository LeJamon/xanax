package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"xanax/internal/attach"
	"xanax/internal/session"
	"xanax/internal/store"
)

const logFollowPollInterval = 300 * time.Millisecond

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <session-id>",
		Short: "Print a session's raw output log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}
			st, err := e.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			sess, err := st.GetSession(args[0])
			if err != nil {
				return err
			}
			path := filepath.Join(e.paths.LogsDir, sess.ID+".raw")
			out := cmd.OutOrStdout()
			f, err := os.Open(path)
			if err != nil {
				if follow && sess.Status.Terminal() {
					fmt.Fprintln(cmd.ErrOrStderr(), terminalFollowMessage(sess))
					return nil
				}
				if sess.Status == session.StatusFailed {
					fmt.Fprintln(out, e.failureSummary(st, sess))
					return nil
				}
				return fmt.Errorf("no log for session %s", shortID(sess.ID))
			}
			defer f.Close()

			if !follow {
				pos, _ := io.Copy(out, f)
				if pos == 0 && sess.Status == session.StatusFailed {
					fmt.Fprintln(out, e.failureSummary(st, sess))
				}
				return nil
			}
			return followRawLog(logFollowOptions{
				Store:      st,
				SessionID:  sess.ID,
				SocketPath: e.socketPath(sess.ID),
				File:       f,
				Out:        out,
				ErrOut:     cmd.ErrOrStderr(),
				Interval:   logFollowPollInterval,
				Alive:      attach.Alive,
			})
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing as the log grows (ctrl+c to stop)")
	return cmd
}

type logFollowOptions struct {
	Store      *store.Store
	SessionID  string
	SocketPath string
	File       *os.File
	Out        io.Writer
	ErrOut     io.Writer
	Interval   time.Duration
	Alive      func(string) bool
}

func followRawLog(opts logFollowOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.ErrOut == nil {
		opts.ErrOut = io.Discard
	}
	if opts.Interval <= 0 {
		opts.Interval = logFollowPollInterval
	}
	if opts.Alive == nil {
		opts.Alive = attach.Alive
	}

	var pos int64
	for {
		if err := drainRawLog(opts.File, opts.Out, &pos); err != nil {
			return err
		}
		if msg, err := followEndMessage(opts); err != nil {
			return err
		} else if msg != "" {
			fmt.Fprintln(opts.ErrOut, msg)
			return nil
		}
		time.Sleep(opts.Interval)
	}
}

func drainRawLog(f *os.File, out io.Writer, pos *int64) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < *pos {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		*pos = 0
	}
	n, err := io.Copy(out, f)
	*pos += n
	return err
}

func followEndMessage(opts logFollowOptions) (string, error) {
	sess, err := opts.Store.GetSession(opts.SessionID)
	if err != nil {
		return "", err
	}
	if sess.Status.Terminal() {
		return terminalFollowMessage(sess), nil
	}
	if sess.Status != session.StatusStarting && opts.SocketPath != "" && !opts.Alive(opts.SocketPath) {
		return fmt.Sprintf("Session %s is no longer reachable (last status: %s).", shortID(sess.ID), sess.Status), nil
	}
	return "", nil
}

func terminalFollowMessage(sess *session.Session) string {
	return fmt.Sprintf("Session %s ended (%s).", shortID(sess.ID), sess.Status)
}
