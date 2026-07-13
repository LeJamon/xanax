package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/sessionctl"
	"github.com/LeJamon/rvr/internal/store"
)

func newRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "rm [flags] <session-id>...",
		Aliases: []string{"remove"},
		Short:   "Remove sessions from the list",
		Args:    cobra.MinimumNArgs(1),
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

			removed, err := e.removeSessions(st, args, force)
			if err != nil {
				return err
			}
			if len(removed) == 1 {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", shortID(removed[0].ID))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %d sessions.\n", len(removed))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "kill live sessions before removing them")
	return cmd
}

func newPruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove terminal sessions from the list",
		Args:  cobra.NoArgs,
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

			sessions, err := st.ListSessions()
			if err != nil {
				return err
			}
			var ids []string
			for _, sess := range sessions {
				if sess.Status.Terminal() {
					ids = append(ids, sess.ID)
				}
			}
			out := cmd.OutOrStdout()
			if len(ids) == 0 {
				fmt.Fprintln(out, "No terminal sessions to prune.")
				return nil
			}
			results, err := sessionctl.Remove(st, ids, sessionctl.Options{
				SocketDir:  e.paths.SocketDir,
				SkipActive: true,
			})
			if err != nil {
				return err
			}
			removed := removedSessions(results)
			if len(removed) == 0 {
				fmt.Fprintln(out, "No terminal sessions to prune.")
				return nil
			}
			if len(removed) == 1 {
				fmt.Fprintln(out, "Pruned 1 session.")
				return nil
			}
			fmt.Fprintf(out, "Pruned %d sessions.\n", len(removed))
			return nil
		},
	}
}

func (e *env) removeSessions(st *store.Store, ids []string, force bool) ([]*session.Session, error) {
	results, err := sessionctl.Remove(st, ids, sessionctl.Options{
		SocketDir: e.paths.SocketDir,
		Force:     force,
	})
	if err != nil {
		var active *sessionctl.ActiveError
		if errors.As(err, &active) {
			return nil, fmt.Errorf("session %s %s; use --force to kill and remove it",
				shortID(active.Session.ID), active.Reason)
		}
		return nil, err
	}
	return removedSessions(results), nil
}

func removedSessions(results []sessionctl.Result) []*session.Session {
	removed := make([]*session.Session, 0, len(results))
	for _, result := range results {
		removed = append(removed, result.Session)
	}
	return removed
}
