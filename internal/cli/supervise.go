package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"xanax/internal/supervisor"
)

func newSuperviseCmd() *cobra.Command {
	var resume bool
	cmd := &cobra.Command{
		Use:    "_supervise <session-id>",
		Short:  "Internal per-session supervisor entrypoint",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			e, err := loadEnv()
			if err != nil {
				return err
			}
			st, err := e.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			sess, err := st.GetSession(id)
			if err != nil {
				return err
			}
			harness, ok := e.cfg.Harnesses[sess.Harness]
			if !ok {
				return err
			}

			// stderr is redirected to the supervisor log file by the parent.
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			code, err := supervisor.Run(supervisor.Options{
				Session: sess,
				Harness: harness,
				Paths:   e.paths,
				Store:   st,
				Logger:  logger,
				Resume:  resume,
				Notify:  e.cfg.Notifications,
			})
			if err != nil {
				logger.Error("supervisor exited with error", "err", err)
			}
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&resume, "resume", false, "resume the harness's existing session")
	return cmd
}
