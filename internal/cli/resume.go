package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"xanax/internal/attach"
)

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <session-id>",
		Short: "Reattach to a live session, or relaunch a dead one via the harness's native resume",
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
			out := cmd.OutOrStdout()

			// Already running: just reattach.
			if attach.Alive(e.socketPath(sess.ID)) {
				return runAttach(e, sess.ID)
			}

			// Dead: relaunch via the harness's native resume.
			if !e.canResume(sess) {
				return fmt.Errorf("session %s cannot be resumed (no harness session ref, and its harness has no resume_args)",
					shortID(sess.ID))
			}
			h, ok := e.cfg.Harnesses[sess.Harness]
			if !ok {
				return fmt.Errorf("session %s uses unknown harness %q", shortID(sess.ID), sess.Harness)
			}
			if err := e.checkHarnessCommand(sess.Harness, h); err != nil {
				return err
			}
			if _, err := e.spawnSupervisor(sess.ID, true); err != nil {
				return err
			}
			st.RecordEvent(sess.ID, "resumed", map[string]any{"auto": false})
			fmt.Fprintf(out, "Resuming session %s (%s)...\n", shortID(sess.ID), sess.Harness)
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
}
