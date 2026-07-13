package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/session"
)

func newKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill <session-id>",
		Short: "Terminate a session",
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

			if attach.Alive(e.socketPath(sess.ID)) {
				if err := attach.Kill(e.socketPath(sess.ID)); err != nil {
					return err
				}
				fmt.Fprintf(out, "Sent kill to %s.\n", shortID(sess.ID))
				return nil
			}
			if sess.Status.Terminal() {
				fmt.Fprintf(out, "Session %s already ended (%s).\n", shortID(sess.ID), sess.Status)
				return nil
			}
			// Live status but no supervisor: mark it cancelled directly.
			st.SetStatus(sess.ID, session.StatusCancelled, "killed while detached")
			st.RecordEvent(sess.ID, "killed", map[string]any{"orphaned": true})
			fmt.Fprintf(out, "Session %s was not running; marked cancelled.\n", shortID(sess.ID))
			return nil
		},
	}
}
