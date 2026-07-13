package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func TestCanResume(t *testing.T) {
	e := &env{cfg: &config.Config{Harnesses: map[string]config.Harness{
		"opencode": {Adapter: "opencode"},                            // no resume_args
		"goose":    {Adapter: "generic", ResumeArgs: []string{"-c"}}, // continue flag
		"aider":    {Adapter: "generic"},                             // nothing
	}}}

	cases := []struct {
		name string
		sess *session.Session
		want bool
	}{
		{"captured ref for configured harness", &session.Session{Harness: "opencode", HarnessSessionRef: "ses_1"}, true},
		{"generic with resume_args", &session.Session{Harness: "goose"}, true},
		{"generic without resume_args", &session.Session{Harness: "aider"}, false},
		{"native without ref", &session.Session{Harness: "opencode"}, false},
		{"unknown harness", &session.Session{Harness: "gone"}, false},
		{"captured ref for unknown harness", &session.Session{Harness: "gone", HarnessSessionRef: "ses_1"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := e.canResume(c.sess); got != c.want {
				t.Errorf("canResume = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCheckHarnessCommandMissing(t *testing.T) {
	e := &env{paths: config.Paths{ConfigFile: "/tmp/rvr-config.toml"}}
	err := e.checkHarnessCommand("opencode", config.Harness{Command: "rvr-definitely-missing-command"})
	if err == nil {
		t.Fatal("checkHarnessCommand succeeded for a missing command")
	}
	msg := err.Error()
	if !strings.Contains(msg, `harness "opencode" command "rvr-definitely-missing-command" is not available`) {
		t.Fatalf("error did not name the missing harness command: %v", err)
	}
	if !strings.Contains(msg, "[harness.opencode].command") || !strings.Contains(msg, e.paths.ConfigFile) {
		t.Fatalf("error did not include config fix hint: %v", err)
	}
}

func TestCheckHarnessCommandAvailable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{paths: config.Paths{ConfigFile: "/tmp/rvr-config.toml"}}
	if err := e.checkHarnessCommand("test", config.Harness{Command: exe}); err != nil {
		t.Fatalf("checkHarnessCommand rejected executable test binary: %v", err)
	}
}

func TestFailureSummaryUsesStatusDetail(t *testing.T) {
	st, sess := failedSession(t, "start harness failed: exec: opencode: not found")
	e := &env{paths: config.Paths{LogsDir: "/tmp/rvr-logs"}}

	got := e.failureSummary(st, sess)
	if !strings.Contains(got, "start harness failed: exec: opencode: not found") {
		t.Fatalf("summary missing status detail: %q", got)
	}
	if !strings.Contains(got, "/tmp/rvr-logs/"+sess.ID+".supervisor.log") {
		t.Fatalf("summary missing supervisor log path: %q", got)
	}
}

func TestFailureDetailFallsBackToErrorEvent(t *testing.T) {
	st, sess := failedSession(t, "")
	if err := st.RecordEvent(sess.ID, "error", map[string]any{"message": "adapter init failed: boom"}); err != nil {
		t.Fatal(err)
	}

	if got := failureDetail(st, sess); got != "adapter init failed: boom" {
		t.Fatalf("failureDetail = %q, want error event message", got)
	}
}

func TestWaitForSocketOrTerminalReturnsWhenSessionIsRemoved(t *testing.T) {
	st, sess := failedSession(t, "")
	if err := st.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	e := &env{paths: config.Paths{SocketDir: t.TempDir()}}
	start := time.Now()
	_, _, err := e.waitForSocketOrTerminal(st, sess.ID, time.Second)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wait error = %v, want ErrNotFound", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("missing session wait took %s", elapsed)
	}
}

func failedSession(t *testing.T, detail string) (*store.Store, *session.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rvr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sess := &session.Session{
		ID:       "fail0001-0000-0000-0000-000000000001",
		Title:    "failed",
		RepoPath: t.TempDir(),
		Harness:  "opencode",
		Status:   session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishWithDetail(sess.ID, session.StatusFailed, 1, detail); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	return st, got
}
