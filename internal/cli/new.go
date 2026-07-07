package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"xanax/internal/attach"
	"xanax/internal/session"
)

func newNewCmd() *cobra.Command {
	var (
		harness  string
		repo     string
		title    string
		noAttach bool
	)
	cmd := &cobra.Command{
		Use:   `new [flags] "prompt"`,
		Short: "Launch a new agent session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := args[0]
			e, err := loadEnv()
			if err != nil {
				return err
			}

			harnessName := harness
			if harnessName == "" {
				harnessName = e.cfg.DefaultHarness
			}
			h, ok := e.cfg.Harnesses[harnessName]
			if !ok {
				return fmt.Errorf("unknown harness %q (see `xanax config`)", harnessName)
			}
			if err := e.checkHarnessCommand(harnessName, h); err != nil {
				return err
			}

			repoAbs, err := filepath.Abs(repo)
			if err != nil {
				return err
			}
			if fi, err := os.Stat(repoAbs); err != nil || !fi.IsDir() {
				return fmt.Errorf("repo %q is not a directory", repoAbs)
			}

			st, err := e.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			sess := &session.Session{
				ID:            uuid.NewString(),
				Title:         deriveTitle(title, prompt),
				RepoPath:      repoAbs,
				Branch:        gitBranch(repoAbs),
				Harness:       harnessName,
				InitialPrompt: prompt,
				Status:        session.StatusStarting,
				SocketPath:    e.socketPath(""), // placeholder; real path set below
			}
			sess.SocketPath = e.socketPath(sess.ID)

			if err := st.CreateSession(sess); err != nil {
				return fmt.Errorf("persist session: %w", err)
			}
			st.RecordEvent(sess.ID, "created", map[string]any{"harness": harnessName})
			st.TouchRepository(repoAbs, filepath.Base(repoAbs))

			if _, err := e.spawnSupervisor(sess.ID, false); err != nil {
				st.FinishWithDetail(sess.ID, session.StatusFailed, 1, err.Error())
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Started session %s (%s) in %s\n",
				shortID(sess.ID), harnessName, filepath.Base(repoAbs))

			wantAttach := !noAttach && term.IsTerminal(int(os.Stdout.Fd()))
			if !wantAttach {
				fmt.Fprintf(out, "Attach with: xanax attach %s\n", shortID(sess.ID))
				return nil
			}
			wait := 10 * time.Second
			if alive, terminal := e.waitForSocketOrTerminal(st, sess.ID, wait); !alive {
				if terminal != nil {
					return e.sessionUnavailableError(st, terminal)
				}
				return e.supervisorStartingError(sess.ID, wait)
			}
			return runAttach(e, sess.ID)
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "harness to launch (default from config)")
	cmd.Flags().StringVar(&repo, "repo", ".", "repository to run the agent in")
	cmd.Flags().StringVar(&title, "title", "", "session title (default: derived from prompt)")
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, "do not attach after launching")
	return cmd
}

// runAttach connects to a live session and proxies the terminal.
func runAttach(e *env, id string) error {
	res, err := attach.Run(attach.Options{
		SocketPath: e.socketPath(id),
		ExitKey:    attach.ParseExitKey(e.cfg.InteractExitKey),
	})
	if err != nil {
		return err
	}
	switch res {
	case attach.Detached:
		fmt.Fprintf(os.Stdout, "\r\nDetached from %s (still running).\r\n", shortID(id))
	case attach.SessionExited:
		fmt.Fprintf(os.Stdout, "\r\nSession %s ended.\r\n", shortID(id))
	case attach.Disconnected:
		fmt.Fprintf(os.Stdout, "\r\nDisconnected from %s.\r\n", shortID(id))
	}
	return nil
}
