package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func TestFollowRawLogExitsAfterTerminalSessionDrains(t *testing.T) {
	st, sess, logPath := loggedSession(t, session.StatusRunning, "done")
	if err := st.Finish(sess.ID, session.StatusCompleted, 0); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := followRawLog(logFollowOptions{
		Store:      st,
		SessionID:  sess.ID,
		SocketPath: "/tmp/xanax-test.sock",
		File:       openLog(t, logPath),
		Out:        &out,
		ErrOut:     &errOut,
		Interval:   time.Millisecond,
		Alive:      func(string) bool { return true },
	}); err != nil {
		t.Fatalf("followRawLog: %v", err)
	}

	if got := out.String(); got != "done" {
		t.Fatalf("stdout = %q, want drained log", got)
	}
	if got := errOut.String(); !strings.Contains(got, "Session logdone0 ended (completed).") {
		t.Fatalf("stderr = %q, want completed session message", got)
	}
}

func TestFollowRawLogDrainsGrowthBeforeTerminalExit(t *testing.T) {
	st, sess, logPath := loggedSession(t, session.StatusRunning, "start\n")
	f := openLog(t, logPath)
	var out, errOut bytes.Buffer
	done := make(chan error, 1)

	go func() {
		done <- followRawLog(logFollowOptions{
			Store:      st,
			SessionID:  sess.ID,
			SocketPath: "/tmp/xanax-test.sock",
			File:       f,
			Out:        &out,
			ErrOut:     &errOut,
			Interval:   time.Millisecond,
			Alive:      func(string) bool { return true },
		})
	}()

	time.Sleep(10 * time.Millisecond)
	appendLog(t, logPath, "end\n")
	if err := st.Finish(sess.ID, session.StatusCompleted, 0); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("followRawLog: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("followRawLog did not exit after terminal status")
	}

	if got := out.String(); got != "start\nend\n" {
		t.Fatalf("stdout = %q, want all log bytes", got)
	}
	if got := errOut.String(); !strings.Contains(got, "Session logdone0 ended (completed).") {
		t.Fatalf("stderr = %q, want completed session message", got)
	}
}

func TestFollowRawLogReportsFailureWhenEmptyLogLaterFails(t *testing.T) {
	st, sess, logPath := loggedSession(t, session.StatusStarting, "")
	f := openLog(t, logPath)
	var out, errOut bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- followRawLog(logFollowOptions{
			Store:      st,
			SessionID:  sess.ID,
			SocketPath: "/tmp/rvr-empty-failure.sock",
			File:       f,
			Out:        &out,
			ErrOut:     &errOut,
			Interval:   time.Millisecond,
			Alive:      func(string) bool { return false },
			FailureSummary: func(sess *session.Session) string {
				return "failure detail: " + sess.StatusDetail
			},
		})
	}()
	time.Sleep(10 * time.Millisecond)
	if err := st.FinishWithDetail(sess.ID, session.StatusFailed, 1, "listen failed"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("follow did not return after empty log failed")
	}
	if got := out.String(); got != "failure detail: listen failed\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := errOut.String(); got != "Session logdone0 ended (failed).\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestFollowRawLogExitsWhenLiveSessionSocketDisappears(t *testing.T) {
	st, sess, logPath := loggedSession(t, session.StatusRunning, "still here\n")

	var out, errOut bytes.Buffer
	if err := followRawLog(logFollowOptions{
		Store:      st,
		SessionID:  sess.ID,
		SocketPath: "/tmp/xanax-test.sock",
		File:       openLog(t, logPath),
		Out:        &out,
		ErrOut:     &errOut,
		Interval:   time.Millisecond,
		Alive:      func(string) bool { return false },
	}); err != nil {
		t.Fatalf("followRawLog: %v", err)
	}

	if got := out.String(); got != "still here\n" {
		t.Fatalf("stdout = %q, want drained log", got)
	}
	if got := errOut.String(); !strings.Contains(got, "Session logdone0 is no longer reachable (last status: running).") {
		t.Fatalf("stderr = %q, want unreachable session message", got)
	}
}

func TestLogsFollowCommandReturnsWhenTerminalLogDrained(t *testing.T) {
	paths, sess := commandLogSession(t, "finished\n", true)

	var out, errOut bytes.Buffer
	cmd := newLogsCmd()
	cmd.SetArgs([]string{"-f", sess.ID})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	runLogsCommand(t, cmd)

	if got := out.String(); got != "finished\n" {
		t.Fatalf("stdout = %q, want raw log", got)
	}
	wantErr := "Session cmdlog00 ended (completed).\n"
	if got := errOut.String(); got != wantErr {
		t.Fatalf("stderr = %q, want %q", got, wantErr)
	}
	if _, err := os.Stat(filepath.Join(paths.LogsDir, sess.ID+".raw")); err != nil {
		t.Fatalf("raw log missing after command: %v", err)
	}
}

func TestLogsFollowCommandReturnsWhenTerminalLogMissing(t *testing.T) {
	_, sess := commandLogSession(t, "", false)

	var out, errOut bytes.Buffer
	cmd := newLogsCmd()
	cmd.SetArgs([]string{"-f", sess.ID})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	runLogsCommand(t, cmd)

	if got := out.String(); got != "" {
		t.Fatalf("stdout = %q, want no raw log output", got)
	}
	wantErr := "Session cmdlog00 ended (completed).\n"
	if got := errOut.String(); got != wantErr {
		t.Fatalf("stderr = %q, want %q", got, wantErr)
	}
}

func TestLogsFollowCommandPreservesFailureSummaryWhenLogMissing(t *testing.T) {
	const detail = "start harness failed: executable not found"
	paths, sess := commandLogSessionWithTerminal(t, "", false, session.StatusFailed, detail)

	var out, errOut bytes.Buffer
	cmd := newLogsCmd()
	cmd.SetArgs([]string{"-f", sess.ID})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	runLogsCommand(t, cmd)

	wantOut := "session cmdlog00 failed: " + detail + "\nSupervisor log: " +
		filepath.Join(paths.LogsDir, sess.ID+".supervisor.log") + "\n"
	if got := out.String(); got != wantOut {
		t.Fatalf("stdout = %q, want %q", got, wantOut)
	}
	wantErr := "Session cmdlog00 ended (failed).\n"
	if got := errOut.String(); got != wantErr {
		t.Fatalf("stderr = %q, want %q", got, wantErr)
	}
}

func TestLogsFollowCommandPreservesFailureSummaryWhenLogEmpty(t *testing.T) {
	const detail = "socket listen failed: address already in use"
	paths, sess := commandLogSessionWithTerminal(t, "", true, session.StatusFailed, detail)

	var out, errOut bytes.Buffer
	cmd := newLogsCmd()
	cmd.SetArgs([]string{"-f", sess.ID})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	runLogsCommand(t, cmd)

	wantOut := "session cmdlog00 failed: " + detail + "\nSupervisor log: " +
		filepath.Join(paths.LogsDir, sess.ID+".supervisor.log") + "\n"
	if got := out.String(); got != wantOut {
		t.Fatalf("stdout = %q, want %q", got, wantOut)
	}
	if got, want := errOut.String(), "Session cmdlog00 ended (failed).\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func loggedSession(t *testing.T, status session.Status, log string) (*store.Store, *session.Session, string) {
	t.Helper()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "xanax.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sess := &session.Session{
		ID:       "logdone0-0000-0000-0000-000000000001",
		Title:    "log follow",
		RepoPath: dir,
		Harness:  "opencode",
		Status:   status,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	logPath := filepath.Join(dir, sess.ID+".raw")
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	return st, sess, logPath
}

func commandLogSession(t *testing.T, log string, writeLog bool) (config.Paths, *session.Session) {
	return commandLogSessionWithTerminal(t, log, writeLog, session.StatusCompleted, "")
}

func commandLogSessionWithTerminal(
	t *testing.T,
	log string,
	writeLog bool,
	terminalStatus session.Status,
	detail string,
) (config.Paths, *session.Session) {
	t.Helper()

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
		ID:       "cmdlog00-0000-0000-0000-000000000001",
		Title:    "log command",
		RepoPath: root,
		Harness:  "opencode",
		Status:   session.StatusRunning,
	}
	if err := st.CreateSession(sess); err != nil {
		st.Close()
		t.Fatalf("CreateSession: %v", err)
	}
	exitCode := 0
	if terminalStatus == session.StatusFailed {
		exitCode = 1
	}
	if err := st.FinishWithDetail(sess.ID, terminalStatus, exitCode, detail); err != nil {
		st.Close()
		t.Fatalf("FinishWithDetail: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	if writeLog {
		if err := os.MkdirAll(paths.LogsDir, 0o700); err != nil {
			t.Fatalf("mkdir logs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(paths.LogsDir, sess.ID+".raw"), []byte(log), 0o600); err != nil {
			t.Fatalf("write raw log: %v", err)
		}
	}
	return paths, sess
}

func runLogsCommand(t *testing.T, cmd interface{ Execute() error }) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("logs command did not exit")
	}
}

func openLog(t *testing.T, path string) *os.File {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open raw log: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func appendLog(t *testing.T, path, s string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append raw log: %v", err)
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		t.Fatalf("append raw log: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close raw log: %v", err)
	}
}
