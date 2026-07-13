package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func isolateXDG(t *testing.T) config.Paths {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatalf("default paths: %v", err)
	}
	return paths
}

func TestSuperviseFailsSessionWithMissingHarness(t *testing.T) {
	paths := isolateXDG(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sess := &session.Session{
		ID:                "missing-supervise-session",
		Title:             "missing harness",
		RepoPath:          t.TempDir(),
		Harness:           "gone",
		HarnessSessionRef: "ses_1",
		Status:            session.StatusRunning,
	}
	createCLITestSession(t, st, sess)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"_supervise", sess.ID})

	err = cmd.Execute()
	wantDetail := missingHarnessDetail("gone")
	if err == nil {
		t.Fatal("_supervise succeeded, want missing-harness error")
	}
	if !strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("error = %q, want %q", err, wantDetail)
	}

	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Status != session.StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.StatusDetail != wantDetail {
		t.Fatalf("status detail = %q, want %q", got.StatusDetail, wantDetail)
	}
	if got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("exit code = %v, want 1", got.ExitCode)
	}
	if got.EndedAt == nil {
		t.Fatal("ended_at is nil, want terminal timestamp")
	}
}
