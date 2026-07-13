package sessionctl

import (
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
)

func TestForcedRemoveWaitsForSocketlessSupervisorLease(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusStarting)
	lease := acquireLease(t, socketDir, sess.ID)

	done := make(chan error, 1)
	go func() {
		_, err := Remove(st, []string{sess.ID}, Options{
			SocketDir:    socketDir,
			Force:        true,
			StopTimeout:  time.Second,
			PollInterval: time.Millisecond,
			Alive:        func(string) bool { return false },
		})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("remove returned while supervisor lease was held: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("session was deleted before lease handoff: %v", err)
	}

	lease.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("remove after lease release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("remove did not finish after lease release")
	}
	if _, err := st.GetSession(sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup error = %v, want ErrNotFound", err)
	}
}

func TestPruneSkipsTerminalSessionWithSupervisorLease(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusCompleted)
	lease := acquireLease(t, socketDir, sess.ID)
	defer lease.Release()

	removed, err := Remove(st, []string{sess.ID}, Options{
		SocketDir:  socketDir,
		SkipActive: true,
		Alive:      func(string) bool { return false },
	})
	if err != nil {
		t.Fatalf("prune remove: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed %d sessions, want held session skipped", len(removed))
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("prune deleted concurrently resumed session: %v", err)
	}
}

func TestPruneDoesNotDeleteLifecycleThatResumesAfterSelection(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusCompleted)
	if err := st.RecordEvent(sess.ID, "completed", nil); err != nil {
		t.Fatal(err)
	}
	resumed := false
	removed, err := Remove(st, []string{sess.ID}, Options{
		SocketDir:  socketDir,
		SkipActive: true,
		Alive: func(string) bool {
			if err := st.BeginResume(sess); err != nil {
				t.Fatalf("BeginResume during prune: %v", err)
			}
			resumed = true
			return false
		},
	})
	if err != nil {
		t.Fatalf("prune remove: %v", err)
	}
	if !resumed {
		t.Fatal("test did not resume the selected terminal lifecycle")
	}
	if len(removed) != 0 {
		t.Fatalf("prune removed resumed lifecycle: %+v", removed)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("resumed session was deleted: %v", err)
	}
	if got.Status != session.StatusStarting {
		t.Fatalf("resumed status = %q, want starting", got.Status)
	}
	events, err := st.ListEvents(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "completed" {
		t.Fatalf("resumed lifecycle events = %+v, want completed event retained", events)
	}
}

func TestForcedRemoveKillsReachableOwnerBeforeLeaseHandoff(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusRunning)
	lease := acquireLease(t, socketDir, sess.ID)
	var killed atomic.Bool

	removed, err := Remove(st, []string{sess.ID}, Options{
		SocketDir:    socketDir,
		Force:        true,
		StopTimeout:  time.Second,
		PollInterval: time.Millisecond,
		Alive:        func(string) bool { return !killed.Load() },
		Kill: func(string) error {
			killed.Store(true)
			lease.Release()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if !killed.Load() {
		t.Fatal("forced remove did not request owner termination")
	}
	if len(removed) != 1 || !removed[0].Killed {
		t.Fatalf("remove result = %+v, want one killed session", removed)
	}
	if _, err := st.GetSession(sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup error = %v, want ErrNotFound", err)
	}
}

func TestForcedRemoveKillsReplacementOwnerAfterHandoff(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusRunning)
	first := acquireLease(t, socketDir, sess.ID)
	var second *supervisor.Lease
	var kills atomic.Int32

	removed, err := Remove(st, []string{sess.ID}, Options{
		SocketDir:    socketDir,
		Force:        true,
		StopTimeout:  time.Second,
		PollInterval: time.Millisecond,
		Alive:        func(string) bool { return true },
		Kill: func(string) error {
			switch kills.Add(1) {
			case 1:
				first.Release()
				second = acquireLease(t, socketDir, sess.ID)
			case 2:
				second.Release()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("forced remove across owner handoff: %v", err)
	}
	if got := kills.Load(); got < 2 {
		t.Fatalf("kill requests = %d, want replacement owner killed", got)
	}
	if len(removed) != 1 || !removed[0].Killed {
		t.Fatalf("remove result = %+v, want one killed session", removed)
	}
	if _, err := st.GetSession(sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup error = %v, want ErrNotFound", err)
	}
}

func TestRemoveWithoutForceRejectsLeaseOwner(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusCompleted)
	lease := acquireLease(t, socketDir, sess.ID)
	defer lease.Release()

	_, err := Remove(st, []string{sess.ID}, Options{
		SocketDir: socketDir,
		Alive:     func(string) bool { return false },
	})
	if !errors.Is(err, ErrActive) {
		t.Fatalf("remove error = %v, want ErrActive", err)
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("unforced remove deleted owned session: %v", err)
	}
}

func TestForcedRemoveTimeoutLeavesSessionRecorded(t *testing.T) {
	st, socketDir, sess := testSession(t, session.StatusStarting)
	lease := acquireLease(t, socketDir, sess.ID)
	defer lease.Release()

	_, err := Remove(st, []string{sess.ID}, Options{
		SocketDir:    socketDir,
		Force:        true,
		StopTimeout:  10 * time.Millisecond,
		PollInterval: time.Millisecond,
		Alive:        func(string) bool { return false },
	})
	if err == nil || !strings.Contains(err.Error(), "supervisor did not stop") {
		t.Fatalf("forced remove error = %v, want bounded stop timeout", err)
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("timed-out remove deleted session: %v", err)
	}
}

func TestPruneReleasesLeasesForMixedSkippedAndRemovedTargets(t *testing.T) {
	st, socketDir, skipped := testSession(t, session.StatusCompleted)
	removedID := "remove-second-session"
	if err := st.CreateSession(&session.Session{
		ID: removedID, Title: "remove", RepoPath: t.TempDir(), Harness: "generic",
		Status: session.StatusStarting,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(removedID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}

	results, err := Remove(st, []string{skipped.ID, removedID}, Options{
		SocketDir:  socketDir,
		SkipActive: true,
		Alive: func(path string) bool {
			return strings.Contains(path, skipped.ID)
		},
	})
	if err != nil {
		t.Fatalf("mixed prune: %v", err)
	}
	if len(results) != 1 || results[0].Session.ID != removedID {
		t.Fatalf("mixed prune results = %+v, want only %s", results, removedID)
	}
	if _, err := st.GetSession(skipped.ID); err != nil {
		t.Fatalf("mixed prune deleted skipped session: %v", err)
	}
	if _, err := st.GetSession(removedID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mixed prune retained removed session: %v", err)
	}

	for _, id := range []string{skipped.ID, removedID} {
		lease := acquireLease(t, socketDir, id)
		lease.Release()
	}
}

func TestPruneReleasesEachLeaseBeforeProcessingNextTarget(t *testing.T) {
	st, socketDir, first := testSession(t, session.StatusCompleted)
	secondID := "remove-next-session"
	if err := st.CreateSession(&session.Session{
		ID: secondID, Title: "second", RepoPath: t.TempDir(), Harness: "generic",
		Status: session.StatusStarting,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(secondID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	checked := false
	_, err := Remove(st, []string{first.ID, secondID}, Options{
		SocketDir:  socketDir,
		SkipActive: true,
		Alive: func(path string) bool {
			if strings.Contains(path, secondID) {
				lease := acquireLease(t, socketDir, first.ID)
				lease.Release()
				checked = true
			}
			return false
		},
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !checked {
		t.Fatal("test did not inspect the first lease while processing the second target")
	}
}

func TestUnforcedRemoveReleasesEachLeaseBeforeProcessingNextTarget(t *testing.T) {
	st, socketDir, first := testSession(t, session.StatusCompleted)
	secondID := "remove-next-unforced"
	if err := st.CreateSession(&session.Session{
		ID: secondID, Title: "second", RepoPath: t.TempDir(), Harness: "generic",
		Status: session.StatusStarting,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(secondID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	checked := false
	_, err := Remove(st, []string{first.ID, secondID}, Options{
		SocketDir: socketDir,
		Alive: func(path string) bool {
			if strings.Contains(path, secondID) {
				lease := acquireLease(t, socketDir, first.ID)
				lease.Release()
				checked = true
			}
			return false
		},
	})
	if err != nil {
		t.Fatalf("unforced remove: %v", err)
	}
	if !checked {
		t.Fatal("test did not inspect the first lease while processing the second target")
	}
}

func testSession(t *testing.T, status session.Status) (*store.Store, string, *session.Session) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rvr.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sess := &session.Session{
		ID:       "remove-lease-session",
		Title:    "remove lease",
		RepoPath: dir,
		Harness:  "generic",
		Status:   session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if status.Terminal() {
		if err := st.Finish(sess.ID, status, 0); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	} else if status != session.StatusStarting {
		if err := st.SetStatus(sess.ID, status, ""); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return st, filepath.Join(dir, "sockets"), got
}

func acquireLease(t *testing.T, socketDir, id string) *supervisor.Lease {
	t.Helper()
	lease, err := supervisor.TryAcquireLease(filepath.Join(socketDir, id+".sock"))
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	return lease
}
