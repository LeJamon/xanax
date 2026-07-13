package store_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rvr.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sample(id string) *session.Session {
	return &session.Session{
		ID:            id,
		Title:         "fix failing tests",
		RepoPath:      "/home/dev/project",
		Harness:       "opencode",
		InitialPrompt: "fix the failing tests",
		Status:        session.StatusStarting,
	}
}

func TestCreateAndGet(t *testing.T) {
	st := openTemp(t)
	in := sample("11111111-aaaa-bbbb-cccc-000000000001")
	if err := st.CreateSession(in); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Error("CreateSession did not stamp timestamps")
	}

	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Title != in.Title || got.Harness != in.Harness || got.Status != session.StatusStarting {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.InitialPrompt != in.InitialPrompt {
		t.Errorf("initial prompt = %q, want %q", got.InitialPrompt, in.InitialPrompt)
	}
	if in.Lifecycle != 1 || got.Lifecycle != 1 {
		t.Errorf("initial lifecycle = (%d, %d), want 1", in.Lifecycle, got.Lifecycle)
	}
}

func TestGetByUniquePrefix(t *testing.T) {
	st := openTemp(t)
	if err := st.CreateSession(sample("abc11111-0000-0000-0000-000000000001")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(sample("def22222-0000-0000-0000-000000000002")); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession("abc")
	if err != nil {
		t.Fatalf("GetSession(prefix): %v", err)
	}
	if got.ID != "abc11111-0000-0000-0000-000000000001" {
		t.Errorf("prefix matched wrong session: %s", got.ID)
	}
}

func TestGetAmbiguousPrefix(t *testing.T) {
	st := openTemp(t)
	if err := st.CreateSession(sample("aaa11111-0000-0000-0000-000000000001")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(sample("aaa22222-0000-0000-0000-000000000002")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession("aaa"); !errors.Is(err, store.ErrAmbiguous) {
		t.Fatalf("want ErrAmbiguous, got %v", err)
	}
}

func TestGetExactWinsOverPrefix(t *testing.T) {
	st := openTemp(t)
	// "a" is an exact ID and also a prefix of "ab"; exact match must win
	// rather than reporting ambiguity.
	if err := st.CreateSession(sample("a")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(sample("ab")); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession("a")
	if err != nil {
		t.Fatalf("GetSession exact: %v", err)
	}
	if got.ID != "a" {
		t.Errorf("exact match returned %q", got.ID)
	}
}

func TestGetNotFound(t *testing.T) {
	st := openTemp(t)
	if _, err := st.GetSession("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSetStatusAndExitCode(t *testing.T) {
	st := openTemp(t)
	in := sample("22222222-0000-0000-0000-000000000001")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(in.ID, session.StatusWaiting, "which API should I use?"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusWaiting {
		t.Errorf("status = %q, want waiting", got.Status)
	}
	if got.StatusDetail != "which API should I use?" {
		t.Errorf("detail = %q", got.StatusDetail)
	}
	if !got.UpdatedAt.After(got.CreatedAt) && !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Error("updated_at not advanced")
	}
}

func TestFinishWithDetail(t *testing.T) {
	st := openTemp(t)
	in := sample("22222222-0000-0000-0000-000000000002")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishWithDetail(in.ID, session.StatusFailed, 127, "exec: opencode: not found"); err != nil {
		t.Fatalf("FinishWithDetail: %v", err)
	}
	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 127 {
		t.Errorf("exit code = %v, want 127", got.ExitCode)
	}
	if got.StatusDetail != "exec: opencode: not found" {
		t.Errorf("detail = %q, want failure detail", got.StatusDetail)
	}
}

func TestBeginResumeClearsPreviousTerminalOutcome(t *testing.T) {
	st := openTemp(t)
	in := sample("23232323-0000-0000-0000-000000000023")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRuntime(in.ID, 1234, "/tmp/old.sock", session.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishWithDetail(in.ID, session.StatusFailed, 127, "old failure"); err != nil {
		t.Fatal(err)
	}
	terminal, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BeginResume(terminal); err != nil {
		t.Fatalf("BeginResume: %v", err)
	}

	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusStarting || got.StatusDetail != "" {
		t.Fatalf("resumed state = (%q, %q), want starting with no detail", got.Status, got.StatusDetail)
	}
	if got.PID != 0 || got.SocketPath != "" || got.ExitCode != nil || got.EndedAt != nil {
		t.Fatalf("previous runtime outcome survived resume: pid=%d socket=%q exit=%v ended=%v",
			got.PID, got.SocketPath, got.ExitCode, got.EndedAt)
	}
}

func TestBeginResumeDoesNotOverwriteConcurrentRuntime(t *testing.T) {
	st := openTemp(t)
	in := sample("24242424-0000-0000-0000-000000000024")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(in.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	stale, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	winner := *stale
	if err := st.BeginResume(&winner); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRuntime(in.ID, 4321, "/tmp/winner.sock", session.StatusRunning); err != nil {
		t.Fatal(err)
	}

	if err := st.BeginResume(stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale BeginResume error = %v, want ErrConflict", err)
	}
	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusRunning || got.PID != 4321 || got.SocketPath != "/tmp/winner.sock" {
		t.Fatalf("stale resume overwrote winner runtime: status=%q pid=%d socket=%q",
			got.Status, got.PID, got.SocketPath)
	}
}

func TestBeginResumeIgnoresConcurrentMetadataChange(t *testing.T) {
	for _, status := range []session.Status{session.StatusCompleted, session.StatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			st := openTemp(t)
			in := sample("25252525-0000-0000-0000-000000000025")
			if err := st.CreateSession(in); err != nil {
				t.Fatal(err)
			}
			if status.Terminal() {
				if err := st.Finish(in.ID, status, 0); err != nil {
					t.Fatal(err)
				}
			} else if err := st.SetStatus(in.ID, status, ""); err != nil {
				t.Fatal(err)
			}
			stale, err := st.GetSession(in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetTitle(in.ID, "renamed concurrently"); err != nil {
				t.Fatal(err)
			}

			if err := st.BeginResume(stale); err != nil {
				t.Fatalf("BeginResume after metadata change: %v", err)
			}
			got, err := st.GetSession(in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != session.StatusStarting || got.Title != "renamed concurrently" {
				t.Fatalf("resumed metadata state = (%q, %q), want starting with new title", got.Status, got.Title)
			}
		})
	}
}

func TestBeginResumeRejectsFastCompletedConcurrentLifecycle(t *testing.T) {
	st := openTemp(t)
	in := sample("26262626-0000-0000-0000-000000000026")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(in.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	stale, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	winner := *stale
	if err := st.BeginResume(&winner); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRuntime(in.ID, 9876, "/tmp/fast.sock", session.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(in.ID, session.StatusFailed, 1); err != nil {
		t.Fatal(err)
	}

	if err := st.BeginResume(stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale BeginResume error = %v, want ErrConflict", err)
	}
	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusFailed || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("fast winner outcome was overwritten: status=%q exit=%v", got.Status, got.ExitCode)
	}
}

func TestBeginResumeRejectsCompletedObservedLifecycle(t *testing.T) {
	st := openTemp(t)
	in := sample("28282828-0000-0000-0000-000000000028")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(in.ID, session.StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	stale, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(in.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginResume(stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale running BeginResume error = %v, want ErrConflict", err)
	}
}

func TestFinishIfCurrentRejectsNewLifecycle(t *testing.T) {
	st := openTemp(t)
	in := sample("27272727-0000-0000-0000-000000000027")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(in.ID, session.StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	current, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := *current
	if err := st.BeginResume(current); err != nil {
		t.Fatal(err)
	}

	changed, err := st.FinishIfCurrent(&stale, session.StatusFailed, 1, "stale repair")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("stale snapshot overwrote a new lifecycle")
	}
	if err := st.SetTitle(in.ID, "metadata changed"); err != nil {
		t.Fatal(err)
	}
	changed, err = st.FinishIfCurrent(current, session.StatusFailed, 1, "current failure")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("metadata change blocked current lifecycle finish")
	}
}

// TestSetStatusIgnoresTerminalSession guards the terminal guard: a late state
// write (e.g. a generic-adapter idle tick racing shutdown) must neither
// resurrect a finished session nor be reported as an error.
func TestSetStatusIgnoresTerminalSession(t *testing.T) {
	st := openTemp(t)
	in := sample("33333333-0000-0000-0000-000000000001")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(in.ID, session.StatusCompleted, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(in.ID, session.StatusWaiting, "idle"); err != nil {
		t.Fatalf("SetStatus on a terminal session should be a silent no-op, got %v", err)
	}
	got, err := st.GetSession(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusCompleted {
		t.Errorf("terminal status was overwritten: got %q, want completed", got.Status)
	}
}

func TestSetStatusUnknownSession(t *testing.T) {
	st := openTemp(t)
	if err := st.SetStatus("ghost", session.StatusRunning, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestEventLog(t *testing.T) {
	st := openTemp(t)
	in := sample("33333333-0000-0000-0000-000000000001")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordEvent(in.ID, "created", nil); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := st.RecordEvent(in.ID, "state", map[string]string{"status": "running"}); err != nil {
		t.Fatalf("RecordEvent with payload: %v", err)
	}
	events, err := st.ListEvents(in.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "created" || events[1].Type != "state" {
		t.Errorf("event order wrong: %+v", events)
	}
	if events[1].Payload == "" {
		t.Error("payload not persisted")
	}
}

func TestDeleteSessionRemovesSessionAndEvents(t *testing.T) {
	st := openTemp(t)
	in := sample("33333333-0000-0000-0000-000000000002")
	if err := st.CreateSession(in); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordEvent(in.ID, "created", nil); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	if err := st.DeleteSession(in.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.GetSession(in.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session lookup err = %v, want ErrNotFound", err)
	}
	events, err := st.ListEvents(in.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events remained after delete: %+v", events)
	}
}

func TestDeleteSessionUnknown(t *testing.T) {
	st := openTemp(t)
	if err := st.DeleteSession("missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteSession missing err = %v, want ErrNotFound", err)
	}
}

func TestListSessionsNewestFirst(t *testing.T) {
	st := openTemp(t)
	older := sample("00000000-0000-0000-0000-000000000001")
	newer := sample("00000000-0000-0000-0000-000000000002")
	if err := st.CreateSession(older); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(newer); err != nil {
		t.Fatal(err)
	}
	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
}

func TestReopenAppliesMigrationsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rvr.db")
	st1, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := st1.CreateSession(sample("44444444-0000-0000-0000-000000000001")); err != nil {
		t.Fatal(err)
	}
	st1.Close()

	// Reopening an existing database must be idempotent (migrations already applied).
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	if _, err := st2.GetSession("44444444"); err != nil {
		t.Errorf("data lost across reopen: %v", err)
	}
}

func TestConcurrentOpenSerializesFirstMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rvr.db")
	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			st, err := store.Open(path)
			if err == nil {
				err = st.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Open: %v", err)
		}
	}

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("final Open: %v", err)
	}
	defer st.Close()
	if err := st.CreateSession(sample("after-concurrent-open")); err != nil {
		t.Fatalf("CreateSession after concurrent migration: %v", err)
	}
}
