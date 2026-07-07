package cli

import (
	"log/slog"

	"rvr/internal/attach"
	"rvr/internal/session"
	"rvr/internal/store"
)

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
		if attach.Alive(e.socketPath(s.ID)) {
			continue // still running, nothing to do
		}
		// Supervisor is gone.
		if _, ok := e.cfg.Harnesses[s.Harness]; !ok {
			detail := missingHarnessDetail(s.Harness)
			st.SetStatus(s.ID, session.StatusFailed, detail)
			st.RecordEvent(s.ID, "harness_missing", map[string]any{"harness": s.Harness})
			continue
		}
		if e.cfg.AutoResume && e.canResume(s) {
			if _, err := e.spawnSupervisor(s.ID, true); err != nil {
				slog.Warn("auto-resume failed", "session", s.ID, "err", err)
				st.SetStatus(s.ID, session.StatusFailed, "auto-resume failed")
				continue
			}
			st.RecordEvent(s.ID, "resumed", map[string]any{"auto": true})
			resumed = append(resumed, s.ID)
			continue
		}
		st.SetStatus(s.ID, session.StatusFailed, "orphaned")
		st.RecordEvent(s.ID, "orphaned", nil)
	}
	return resumed, nil
}
