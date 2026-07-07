package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"xanax/internal/session"
)

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
				if sess.Status == session.StatusFailed {
					fmt.Fprintln(out, e.failureSummary(st, sess))
					return nil
				}
				return fmt.Errorf("no log for session %s", shortID(sess.ID))
			}
			defer f.Close()

			pos, _ := io.Copy(out, f)
			if !follow {
				if pos == 0 && sess.Status == session.StatusFailed {
					fmt.Fprintln(out, e.failureSummary(st, sess))
				}
				return nil
			}
			// Follow: poll for growth. The raw log truncate-rotates at its cap,
			// so a shrink means it restarted — rewind to the top.
			for {
				time.Sleep(300 * time.Millisecond)
				fi, err := f.Stat()
				if err != nil {
					return nil
				}
				if fi.Size() < pos {
					if _, err := f.Seek(0, io.SeekStart); err != nil {
						return err
					}
					pos = 0
				}
				n, _ := io.Copy(out, f)
				pos += n
			}
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing as the log grows (ctrl+c to stop)")
	return cmd
}
