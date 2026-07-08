package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xanax/internal/attach"
	"xanax/internal/session"
	"xanax/internal/store"
)

type removeTarget struct {
	sess       *session.Session
	socketPath string
	alive      bool
}

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
				if sess.Status.Terminal() && !attach.Alive(e.socketPath(sess.ID)) {
					ids = append(ids, sess.ID)
				}
			}
			out := cmd.OutOrStdout()
			if len(ids) == 0 {
				fmt.Fprintln(out, "No terminal sessions to prune.")
				return nil
			}
			removed, err := e.removeSessions(st, ids, false)
			if err != nil {
				return err
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
	targets, err := e.resolveRemoveTargets(st, ids)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if !target.needsForce() {
			continue
		}
		if !force {
			return nil, fmt.Errorf("session %s %s; use --force to kill and remove it",
				shortID(target.sess.ID), target.liveReason())
		}
	}
	if force {
		for _, target := range targets {
			if !target.alive {
				continue
			}
			if err := attach.Kill(target.socketPath); err != nil {
				return nil, fmt.Errorf("kill %s before remove: %w", shortID(target.sess.ID), err)
			}
		}
	}

	removed := make([]*session.Session, 0, len(targets))
	for _, target := range targets {
		if err := st.DeleteSession(target.sess.ID); err != nil {
			return nil, fmt.Errorf("remove %s: %w", shortID(target.sess.ID), err)
		}
		removed = append(removed, target.sess)
	}
	return removed, nil
}

func (e *env) resolveRemoveTargets(st *store.Store, ids []string) ([]removeTarget, error) {
	seen := make(map[string]bool, len(ids))
	targets := make([]removeTarget, 0, len(ids))
	for _, id := range ids {
		sess, err := st.GetSession(id)
		if err != nil {
			return nil, err
		}
		if seen[sess.ID] {
			continue
		}
		seen[sess.ID] = true
		socketPath := e.socketPath(sess.ID)
		targets = append(targets, removeTarget{
			sess:       sess,
			socketPath: socketPath,
			alive:      attach.Alive(socketPath),
		})
	}
	return targets, nil
}

func (t removeTarget) needsForce() bool {
	return t.sess.Status.Live() || t.alive
}

func (t removeTarget) liveReason() string {
	if t.sess.Status.Live() {
		return fmt.Sprintf("is %s", t.sess.Status)
	}
	return "is still reachable"
}
