package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
)

func TestRmRemovesTerminalSession(t *testing.T) {
	st := openCLIStore(t)
	sess := createCLISession(t, st, "done0001-0000-0000-0000-000000000001", session.StatusCompleted)
	if err := st.RecordEvent(sess.ID, "created", nil); err != nil {
		t.Fatal(err)
	}

	out, err := executeRoot(t, "rm", "done0001")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	if out != "Removed done0001.\n" {
		t.Fatalf("output = %q", out)
	}
	if _, err := st.GetSession(sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup err = %v, want ErrNotFound", err)
	}
	events, err := st.ListEvents(sess.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events remained after rm: %+v", events)
	}
}

func TestRemoveAliasRemovesTerminalSession(t *testing.T) {
	st := openCLIStore(t)
	createCLISession(t, st, "alias001-0000-0000-0000-000000000001", session.StatusFailed)

	out, err := executeRoot(t, "remove", "alias001")
	if err != nil {
		t.Fatalf("remove alias: %v", err)
	}
	if out != "Removed alias001.\n" {
		t.Fatalf("output = %q", out)
	}
	if _, err := st.GetSession("alias001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup err = %v, want ErrNotFound", err)
	}
}

func TestRmRefusesLiveSessionWithoutForce(t *testing.T) {
	st := openCLIStore(t)
	sess := createCLISession(t, st, "live0001-0000-0000-0000-000000000001", session.StatusRunning)

	out, err := executeRoot(t, "rm", "live0001")
	if err == nil {
		t.Fatal("rm live session succeeded without --force")
	}
	if out != "" {
		t.Fatalf("output = %q, want none on failure", out)
	}
	if !strings.Contains(err.Error(), "session live0001 is running; use --force to kill and remove it") {
		t.Fatalf("error = %v", err)
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("live session was removed despite refusal: %v", err)
	}
}

func TestRmForceRemovesLiveSessionWithoutSocket(t *testing.T) {
	st := openCLIStore(t)
	sess := createCLISession(t, st, "force001-0000-0000-0000-000000000001", session.StatusWaiting)

	out, err := executeRoot(t, "rm", "--force", "force001")
	if err != nil {
		t.Fatalf("rm --force: %v", err)
	}
	if out != "Removed force001.\n" {
		t.Fatalf("output = %q", out)
	}
	if _, err := st.GetSession(sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed session lookup err = %v, want ErrNotFound", err)
	}
}

func TestPruneRemovesOnlyTerminalSessions(t *testing.T) {
	st := openCLIStore(t)
	terminalIDs := []string{
		"done0001-0000-0000-0000-000000000001",
		"fail0001-0000-0000-0000-000000000001",
		"cancel01-0000-0000-0000-000000000001",
	}
	for _, tc := range []struct {
		id     string
		status session.Status
	}{
		{terminalIDs[0], session.StatusCompleted},
		{terminalIDs[1], session.StatusFailed},
		{terminalIDs[2], session.StatusCancelled},
		{"run00001-0000-0000-0000-000000000001", session.StatusRunning},
		{"wait0001-0000-0000-0000-000000000001", session.StatusWaiting},
	} {
		createCLISession(t, st, tc.id, tc.status)
	}

	out, err := executeRoot(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out != "Pruned 3 sessions.\n" {
		t.Fatalf("output = %q", out)
	}
	for _, id := range terminalIDs {
		if _, err := st.GetSession(id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("terminal session %s lookup err = %v, want ErrNotFound", id, err)
		}
	}
	for _, id := range []string{"run00001", "wait0001"} {
		if _, err := st.GetSession(id); err != nil {
			t.Fatalf("live session %s was pruned: %v", id, err)
		}
	}
}

func TestPruneReportsNothingToDo(t *testing.T) {
	st := openCLIStore(t)
	createCLISession(t, st, "run00001-0000-0000-0000-000000000001", session.StatusRunning)

	out, err := executeRoot(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out != "No terminal sessions to prune.\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestRmAndPruneRespectSocketlessSupervisorLease(t *testing.T) {
	st := openCLIStore(t)
	sess := createCLISession(t, st, "owned001-0000-0000-0000-000000000001", session.StatusCompleted)
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := supervisor.TryAcquireLease(filepath.Join(paths.SocketDir, sess.ID+".sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	out, err := executeRoot(t, "rm", "owned001")
	if err == nil || !strings.Contains(err.Error(), "has an active supervisor; use --force") {
		t.Fatalf("rm error = %v, want force requirement for lease owner", err)
	}
	if out != "" {
		t.Fatalf("rm output = %q, want none on refusal", out)
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("rm refusal deleted owned session: %v", err)
	}

	out, err = executeRoot(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out != "No terminal sessions to prune.\n" {
		t.Fatalf("prune output = %q", out)
	}
	if _, err := st.GetSession(sess.ID); err != nil {
		t.Fatalf("prune deleted owned session: %v", err)
	}
}

func openCLIStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func executeRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	return out.String(), err
}

func createCLISession(t *testing.T, st *store.Store, id string, status session.Status) *session.Session {
	t.Helper()
	sess := &session.Session{
		ID:            id,
		Title:         "test session",
		RepoPath:      t.TempDir(),
		Harness:       "opencode",
		InitialPrompt: "test prompt",
		Status:        session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	switch {
	case status == session.StatusStarting:
	case status.Terminal():
		if err := st.Finish(id, status, 0); err != nil {
			t.Fatal(err)
		}
	default:
		if err := st.SetStatus(id, status, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
