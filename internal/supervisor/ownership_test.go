package supervisor

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func TestSupervisorOwnershipIsExclusive(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rvr-own-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	paths := config.Paths{SocketDir: dir}
	opts := Options{Session: &session.Session{ID: "exclusive-session"}, Paths: paths}
	first := &Supervisor{opts: opts}
	if err := first.acquireOwnership(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.releaseOwnership()
	if err := first.listen(); err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer first.cleanupSocket()

	second := &Supervisor{opts: opts}
	if err := second.acquireOwnership(); !errors.Is(err, ErrAlreadySupervised) {
		t.Fatalf("second acquire error = %v, want ErrAlreadySupervised", err)
	}
	if _, err := os.Stat(first.socketPath()); err != nil {
		t.Fatalf("losing supervisor disturbed live socket: %v", err)
	}
	conn, err := net.Dial("unix", first.socketPath())
	if err != nil {
		t.Fatalf("live socket no longer accepts connections: %v", err)
	}
	conn.Close()

	first.cleanupSocket()
	first.releaseOwnership()
	if err := second.acquireOwnership(); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	second.releaseOwnership()
}

func TestResumedSupervisorWaitsForTransientOwnershipLease(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Session: &session.Session{ID: "resume-after-prune"},
		Paths:   config.Paths{SocketDir: dir},
		Resume:  true,
	}
	blocker := &Supervisor{opts: opts}
	if err := blocker.acquireOwnership(); err != nil {
		t.Fatalf("blocker acquire: %v", err)
	}

	resumed := &Supervisor{opts: opts}
	done := make(chan error, 1)
	go func() { done <- resumed.acquireOwnership() }()
	select {
	case err := <-done:
		blocker.releaseOwnership()
		t.Fatalf("resumed supervisor did not wait for transient lease: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	blocker.releaseOwnership()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resumed acquire after release: %v", err)
		}
		resumed.releaseOwnership()
	case <-time.After(time.Second):
		t.Fatal("resumed supervisor did not acquire released lease")
	}
}

func TestLosingSupervisorDoesNotChangeSessionState(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rvr-own-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	paths := config.Paths{SocketDir: dir, DBFile: filepath.Join(dir, "rvr.db")}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer st.Close()
	sess := &session.Session{
		ID: "lease-loser", Title: "owned", RepoPath: dir,
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	owner := &Supervisor{opts: Options{Session: sess, Paths: paths}}
	if err := owner.acquireOwnership(); err != nil {
		t.Fatalf("owner acquire: %v", err)
	}
	defer owner.releaseOwnership()

	code, err := Run(Options{
		Session: sess,
		Harness: config.Harness{Adapter: config.AdapterGeneric, Command: "unused"},
		Paths:   paths,
		Store:   st,
		Logger:  slog.Default(),
	})
	if code != 0 || !errors.Is(err, ErrAlreadySupervised) {
		t.Fatalf("losing Run = (%d, %v), want (0, ErrAlreadySupervised)", code, err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Status != session.StatusStarting || got.EndedAt != nil || got.ExitCode != nil {
		t.Fatalf("losing supervisor changed session: status=%q ended=%v exit=%v", got.Status, got.EndedAt, got.ExitCode)
	}
}

func TestSupervisorDoesNotLaunchAfterSessionWasRemoved(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		SocketDir: filepath.Join(dir, "sockets"),
		LogsDir:   filepath.Join(dir, "logs"),
		DBFile:    filepath.Join(dir, "rvr.db"),
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer st.Close()

	sess := &session.Session{
		ID:       "removed-before-lease",
		Title:    "stale supervisor",
		RepoPath: dir,
		Harness:  "generic",
		Status:   session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.DeleteSession(sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	marker := filepath.Join(dir, "harness-started")
	script := filepath.Join(dir, "harness")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n: > \"$1\"\n"), 0o700); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	code, err := Run(Options{
		Session: sess,
		Harness: config.Harness{
			Adapter: config.AdapterGeneric,
			Command: script,
			Args:    []string{marker},
		},
		Paths:  paths,
		Store:  st,
		Logger: slog.Default(),
	})
	if err != nil || code != 0 {
		t.Fatalf("Run stale supervisor = (%d, %v), want (0, nil)", code, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale supervisor launched harness; marker stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LogsDir, sess.ID+".raw")); !os.IsNotExist(err) {
		t.Fatalf("stale supervisor created raw log; stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.SocketDir, sess.ID+".sock")); !os.IsNotExist(err) {
		t.Fatalf("stale supervisor created socket; stat error = %v", err)
	}
}

func TestDelayedDuplicateResumeDoesNotRelaunchTerminalLifecycle(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		SocketDir: filepath.Join(dir, "sockets"),
		LogsDir:   filepath.Join(dir, "logs"),
		DBFile:    filepath.Join(dir, "rvr.db"),
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{
		ID: "finished-before-lease", Title: "finished", RepoPath: dir,
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(sess.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "duplicate-started")
	script := filepath.Join(dir, "harness")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n: > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	code, err := Run(Options{
		Session: sess,
		Harness: config.Harness{Adapter: config.AdapterGeneric, Command: script, Args: []string{marker}},
		Paths:   paths,
		Store:   st,
		Logger:  slog.Default(),
		Resume:  true,
	})
	if err != nil || code != 0 {
		t.Fatalf("duplicate resumed Run = (%d, %v), want (0, nil)", code, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("duplicate resume launched harness; marker stat error = %v", err)
	}
}
