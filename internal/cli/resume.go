package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
)

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <session-id>",
		Short: "Reattach to a live session, or relaunch a dead one via the harness's native resume",
		Long: `Reattach to a live session, or relaunch a dead one via the harness's native resume.

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
			out := cmd.OutOrStdout()

			for {
				// Already running: just reattach.
				if attach.Alive(e.socketPath(sess.ID)) {
					return runAttach(e, sess.ID)
				}
				if wait := startingHandoffWait(sess, e.now()); wait > 0 {
					if alive, terminal, waitErr := e.waitForSocketOrTerminal(st, sess.ID, wait); waitErr != nil {
						return waitErr
					} else if alive {
						return runAttach(e, sess.ID)
					} else if terminal != nil {
						if terminal.Lifecycle == sess.Lifecycle {
							sess = terminal
							continue
						}
						return e.sessionUnavailableError(st, terminal)
					}
					sess, err = st.GetSession(sess.ID)
					if err != nil {
						return err
					}
					continue
				}

				// Dead: relaunch via the harness's native resume.
				h, ok := e.cfg.Harnesses[sess.Harness]
				if !ok {
					return fmt.Errorf("session %s uses unknown harness %q", shortID(sess.ID), sess.Harness)
				}
				if !e.canResume(sess) {
					return fmt.Errorf("session %s cannot be resumed (no harness session ref, and its harness has no resume_args)",
						shortID(sess.ID))
				}
				if err := e.checkHarnessCommand(sess.Harness, h); err != nil {
					return err
				}
				lease, err := supervisor.TryAcquireLease(e.socketPath(sess.ID))
				ownershipDeadline := time.Now().Add(10 * time.Second)
				for errors.Is(err, supervisor.ErrAlreadySupervised) {
					if attach.Alive(e.socketPath(sess.ID)) {
						return runAttach(e, sess.ID)
					}
					if _, getErr := st.GetSession(sess.ID); getErr != nil {
						return getErr
					}
					if !time.Now().Before(ownershipDeadline) {
						return e.supervisorStartingError(sess.ID, 10*time.Second)
					}
					time.Sleep(50 * time.Millisecond)
					lease, err = supervisor.TryAcquireLease(e.socketPath(sess.ID))
				}
				if err != nil {
					return fmt.Errorf("acquire supervisor lease for session %s: %w", shortID(sess.ID), err)
				}
				fresh, err := st.GetSession(sess.ID)
				if err != nil {
					lease.Release()
					return err
				}
				if fresh.Lifecycle != sess.Lifecycle || fresh.Status != sess.Status {
					lease.Release()
					if fresh.Status.Terminal() {
						if fresh.Lifecycle == sess.Lifecycle {
							sess = fresh
							continue
						}
						return e.sessionUnavailableError(st, fresh)
					}
					sess = fresh
					continue
				}
				sess = fresh
				if err := st.BeginResume(sess); errors.Is(err, store.ErrConflict) {
					lease.Release()
					latest, getErr := st.GetSession(sess.ID)
					if getErr != nil {
						return getErr
					}
					if latest.Status.Terminal() {
						if latest.Lifecycle == sess.Lifecycle {
							sess = latest
							continue
						}
						return e.sessionUnavailableError(st, latest)
					}
					sess = latest
					continue
				} else if err != nil {
					lease.Release()
					return fmt.Errorf("begin resume for session %s: %w", shortID(sess.ID), err)
				}
				if _, err := e.spawnSupervisor(sess.ID, true); err != nil {
					detail := "resume supervisor failed: " + err.Error()
					_, _ = st.FinishIfCurrent(sess, session.StatusFailed, 1, detail)
					lease.Release()
					return err
				}
				lease.Release()
				st.RecordEvent(sess.ID, "resumed", map[string]any{"auto": false})
				fmt.Fprintf(out, "Resuming session %s (%s)...\n", shortID(sess.ID), sess.Harness)
				wait := 10 * time.Second
				if alive, terminal, waitErr := e.waitForSocketOrTerminal(st, sess.ID, wait); !alive {
					if waitErr != nil {
						return waitErr
					}
					if terminal != nil {
						return e.sessionUnavailableError(st, terminal)
					}
					return e.supervisorStartingError(sess.ID, wait)
				}
				return runAttach(e, sess.ID)
			}
		},
	}
}

func startingHandoffWait(sess *session.Session, now time.Time) time.Duration {
	if sess.Status != session.StatusStarting {
		return 0
	}
	age := now.Sub(sess.UpdatedAt)
	if age < 0 {
		age = 0
	}
	if age >= supervisorStartGrace {
		return 0
	}
	return supervisorStartGrace - age
}
