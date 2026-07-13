package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
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

func TestReconcileLeavesRecentStartingLifecycleInFlight(t *testing.T) {
	for _, autoResume := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[autoResume], func(t *testing.T) {
			st := openCLITestStore(t)
			sess := &session.Session{
				ID: "manual-resume-handoff", Title: "resuming", RepoPath: t.TempDir(),
				Harness: "opencode", HarnessSessionRef: "ses_1", Status: session.StatusStarting,
			}
			createCLITestSession(t, st, sess)
			if err := st.Finish(sess.ID, session.StatusCompleted, 0); err != nil {
				t.Fatal(err)
			}
			terminal, err := st.GetSession(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.BeginResume(terminal); err != nil {
				t.Fatal(err)
			}

			e := &env{
				paths: config.Paths{SocketDir: filepath.Join(t.TempDir(), "sockets")},
				cfg: &config.Config{
					AutoResume: autoResume,
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
				t.Fatalf("reconcile spawned duplicate resumes: %v", resumed)
			}
			got, err := st.GetSession(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != session.StatusStarting || got.StatusDetail != "" {
				t.Fatalf("in-flight resume changed to (%q, %q)", got.Status, got.StatusDetail)
			}
		})
	}
}

func TestReconcileRetriesStartingLifecycleAfterGraceExpires(t *testing.T) {
	st := openCLITestStore(t)
	sess := &session.Session{
		ID: "expired-resume-handoff", Title: "resuming", RepoPath: t.TempDir(),
		Harness: "opencode", HarnessSessionRef: "ses_1", Status: session.StatusStarting,
	}
	createCLITestSession(t, st, sess)
	if err := st.Finish(sess.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	terminal, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BeginResume(terminal); err != nil {
		t.Fatal(err)
	}
	starting, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := starting.UpdatedAt
	e := &env{
		paths: config.Paths{SocketDir: filepath.Join(t.TempDir(), "sockets")},
		cfg: &config.Config{
			AutoResume: false,
			Harnesses: map[string]config.Harness{
				"opencode": {Adapter: config.AdapterOpencode},
			},
		},
		nowFn: func() time.Time { return now },
	}
	if _, err := e.reconcile(st); err != nil {
		t.Fatalf("recent reconcile: %v", err)
	}
	now = starting.UpdatedAt.Add(supervisorStartGrace + time.Second)
	if _, err := e.reconcile(st); err != nil {
		t.Fatalf("expired reconcile: %v", err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusFailed || got.StatusDetail != "orphaned" {
		t.Fatalf("expired starting lifecycle = (%q, %q), want failed/orphaned", got.Status, got.StatusDetail)
	}
}

func TestReconcileDoesNotOverwriteConcurrentResume(t *testing.T) {
	st := openCLITestStore(t)
	sess := &session.Session{
		ID: "concurrent-manual-resume", Title: "running", RepoPath: t.TempDir(),
		Harness: "opencode", Status: session.StatusStarting,
	}
	createCLITestSession(t, st, sess)
	if err := st.SetStatus(sess.ID, session.StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	manual, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	e := &env{
		paths: config.Paths{SocketDir: filepath.Join(t.TempDir(), "sockets")},
		cfg: &config.Config{
			AutoResume: false,
			Harnesses:  map[string]config.Harness{"opencode": {Adapter: config.AdapterOpencode}},
		},
		aliveFn: func(string) bool {
			if err := st.BeginResume(manual); err != nil {
				t.Fatalf("concurrent BeginResume: %v", err)
			}
			resumed = true
			return false
		},
	}
	if _, err := e.reconcile(st); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !resumed {
		t.Fatal("test did not start the concurrent lifecycle")
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusStarting || got.StatusDetail != "" {
		t.Fatalf("concurrent resume was overwritten: status=%q detail=%q", got.Status, got.StatusDetail)
	}
}

func TestReconcileSkipsSocketlessSupervisorLeaseOwner(t *testing.T) {
	st := openCLITestStore(t)
	sess := &session.Session{
		ID: "reconcile-owned-session", Title: "owned", RepoPath: t.TempDir(),
		Harness: "opencode", Status: session.StatusStarting,
	}
	createCLITestSession(t, st, sess)
	if err := st.SetStatus(sess.ID, session.StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	socketDir := filepath.Join(t.TempDir(), "sockets")
	lease, err := supervisor.TryAcquireLease(filepath.Join(socketDir, sess.ID+".sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	e := &env{
		paths: config.Paths{SocketDir: socketDir},
		cfg: &config.Config{
			AutoResume: false,
			Harnesses:  map[string]config.Harness{"opencode": {Adapter: config.AdapterOpencode}},
		},
	}
	if _, err := e.reconcile(st); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusRunning {
		t.Fatalf("socketless lease owner status = %q, want running", got.Status)
	}
}

func TestPeriodicReconcileReloadsLatestConfig(t *testing.T) {
	st := openCLITestStore(t)
	sess := &session.Session{
		ID: "new-harness-session", Title: "custom", RepoPath: t.TempDir(),
		Harness: "new-harness", Status: session.StatusStarting,
	}
	createCLITestSession(t, st, sess)
	if err := st.SetStatus(sess.ID, session.StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configFile, []byte("auto_resume = false\n\n[harness.new-harness]\ncommand = \"new-harness\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &env{
		paths: config.Paths{ConfigFile: configFile, SocketDir: filepath.Join(t.TempDir(), "sockets")},
		cfg:   config.Default(), // intentionally lacks the newly saved harness
	}
	if err := e.reconcileLatestConfig(st); err != nil {
		t.Fatalf("periodic reconcile: %v", err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusDetail != "orphaned" {
		t.Fatalf("latest config was not used: status=%q detail=%q", got.Status, got.StatusDetail)
	}
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
