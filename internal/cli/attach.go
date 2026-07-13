package cli

import (
	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/attach"
)

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <session-id>",
		Short: "Attach to a running session",
		Long: `Attach to a running session.

Inside the session window, press Left arrow or ctrl+\ to detach. The session
keeps running after you detach.`,
		Args: cobra.ExactArgs(1),
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
			if !attach.Alive(e.socketPath(sess.ID)) {
				return e.sessionUnavailableError(st, sess)
			}
			return runAttach(e, sess.ID)
		},
	}
}
