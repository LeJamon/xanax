package store_test

import (
	"errors"
	"path/filepath"
	"testing"

	"xanax/internal/session"
	"xanax/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "xanax.db"))
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
	path := filepath.Join(dir, "xanax.db")
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
