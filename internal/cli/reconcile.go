package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
)

const supervisorStartGrace = 15 * time.Second

// reconcile brings the store in line with reality and, when auto_resume is on,
// revives interrupted sessions (SPEC.md §6). A session is "interrupted" when
// its recorded status is live but its supervisor socket is gone. It is
// auto-resumed only if its configured harness can resume it; otherwise it is
// marked failed with the reason. Returns the list of session IDs it resumed.
func (e *env) reconcile(st *store.Store) ([]string, error) {
	sessions, err := st.ListSessions()
	if err != nil {
		return nil, err
	}
	var resumed []string
	for _, s := range sessions {
		if !s.Status.Live() {
			continue
		}
		if e.alive(e.socketPath(s.ID)) {
			continue // still running, nothing to do
		}
		// Creation and resume publish starting before the detached supervisor can
		// bind its socket. A concurrent dashboard must not orphan or duplicate
		// that intentionally visible handoff window.
		if s.Status == session.StatusStarting && e.now().Sub(s.UpdatedAt) < supervisorStartGrace {
			continue
		}
		lease, err := supervisor.TryAcquireLease(e.socketPath(s.ID))
		if errors.Is(err, supervisor.ErrAlreadySupervised) {
			continue
		}
		if err != nil {
			return resumed, fmt.Errorf("acquire supervisor lease for %s: %w", s.ID, err)
		}
		// Supervisor is gone.
		if _, ok := e.cfg.Harnesses[s.Harness]; !ok {
			detail := missingHarnessDetail(s.Harness)
			changed, finishErr := st.FinishIfCurrent(s, session.StatusFailed, 1, detail)
			lease.Release()
			if finishErr != nil {
				return resumed, finishErr
			}
			if changed {
				st.RecordEvent(s.ID, "harness_missing", map[string]any{"harness": s.Harness})
			}
			continue
		}
		if e.cfg.AutoResume && e.canResume(s) {
			if err := st.BeginResume(s); errors.Is(err, store.ErrConflict) {
				lease.Release()
				continue // another reconciler changed or resumed this lifecycle
			} else if err != nil {
				lease.Release()
				slog.Warn("begin auto-resume failed", "session", s.ID, "err", err)
				return resumed, err
			}
			if _, err := e.spawnSupervisor(s.ID, true); err != nil {
				slog.Warn("auto-resume failed", "session", s.ID, "err", err)
				_, finishErr := st.FinishIfCurrent(s, session.StatusFailed, 1, "auto-resume failed")
				lease.Release()
				if finishErr != nil {
					return resumed, finishErr
				}
				continue
			}
			lease.Release()
			st.RecordEvent(s.ID, "resumed", map[string]any{"auto": true})
			resumed = append(resumed, s.ID)
			continue
		}
		changed, finishErr := st.FinishIfCurrent(s, session.StatusFailed, 1, "orphaned")
		lease.Release()
		if finishErr != nil {
			return resumed, finishErr
		}
		if changed {
			st.RecordEvent(s.ID, "orphaned", nil)
		}
	}
	return resumed, nil
}

func (e *env) now() time.Time {
	if e.nowFn != nil {
		return e.nowFn()
	}
	return time.Now()
}

func (e *env) alive(socketPath string) bool {
	if e.aliveFn != nil {
		return e.aliveFn(socketPath)
	}
	return attach.Alive(socketPath)
}
