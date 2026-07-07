package cli

import (
	"path/filepath"
	"testing"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

func openCLITestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "xanax.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func createCLITestSession(t *testing.T, st *store.Store, sess *session.Session) {
	t.Helper()
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func TestReconcileFailsLiveSessionWithMissingHarness(t *testing.T) {
	st := openCLITestStore(t)
	sess := &session.Session{
		ID:                "missing-harness-session",
		Title:             "missing harness",
		RepoPath:          t.TempDir(),
		Harness:           "gone",
		HarnessSessionRef: "ses_1",
		Status:            session.StatusRunning,
	}
	createCLITestSession(t, st, sess)

	e := &env{
		paths: config.Paths{SocketDir: filepath.Join(t.TempDir(), "sockets")},
		cfg: &config.Config{
			AutoResume: true,
			Harnesses: map[string]config.Harness{
				"opencode": {Adapter: config.AdapterOpencode},
			},
		},
	}
	resumed, err := e.reconcile(st)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(resumed) != 0 {
		t.Fatalf("reconcile resumed %v, want none", resumed)
	}

	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	wantDetail := missingHarnessDetail("gone")
	if got.Status != session.StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.StatusDetail != wantDetail {
		t.Fatalf("status detail = %q, want %q", got.StatusDetail, wantDetail)
	}
}
