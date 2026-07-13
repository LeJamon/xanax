package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func TestResumeReportsUnknownHarnessBeforeResumability(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	sess := &session.Session{
		ID:                "removed0-0000-0000-0000-000000000001",
		Title:             "removed harness",
		RepoPath:          root,
		Harness:           "removed",
		HarnessSessionRef: "native-session-ref",
		Status:            session.StatusCompleted,
	}
	if err := st.CreateSession(sess); err != nil {
		st.Close()
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	cmd := newResumeCmd()
	cmd.SetArgs([]string{sess.ID})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("resume succeeded for a removed harness")
	}
	if got, want := err.Error(), `session removed0 uses unknown harness "removed"`; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want it to contain %q", got, want)
	}
}

func TestStartingHandoffWait(t *testing.T) {
	now := time.Now()
	recent := &session.Session{Status: session.StatusStarting, UpdatedAt: now.Add(-time.Second)}
	if wait := startingHandoffWait(recent, now); wait <= 0 || wait >= supervisorStartGrace {
		t.Fatalf("recent starting wait = %s", wait)
	}
	old := &session.Session{Status: session.StatusStarting, UpdatedAt: now.Add(-supervisorStartGrace)}
	if wait := startingHandoffWait(old, now); wait != 0 {
		t.Fatalf("old starting wait = %s, want 0", wait)
	}
	running := &session.Session{Status: session.StatusRunning, UpdatedAt: now}
	if wait := startingHandoffWait(running, now); wait != 0 {
		t.Fatalf("running wait = %s, want 0", wait)
	}
}
