package supervisor_test

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
	"xanax/internal/supervisor"
	"xanax/internal/wire"
)

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	data := t.TempDir()
	// Keep the socket dir short (macOS sun_path limit is 104 bytes).
	sockDir, err := os.MkdirTemp("/tmp", "xnx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	return config.Paths{
		DataDir:   data,
		DBFile:    filepath.Join(data, "xanax.db"),
		LogsDir:   filepath.Join(data, "logs"),
		SocketDir: sockDir,
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitAlive(t *testing.T, sock string) {
	t.Helper()
	for range 100 {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("supervisor socket never came up: %s", sock)
}

// TestSupervisorAttachAndKill exercises the full PTY→ring→socket path: launch a
// `cat` harness, observe the initial prompt echoed back, then kill it via the
// wire protocol and confirm the terminal state.
func TestSupervisorAttachAndKill(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID:            "kill01",
		Title:         "kill test",
		RepoPath:      t.TempDir(),
		Harness:       "generic",
		InitialPrompt: "XANAXPROBE",
		Status:        session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "cat"}

	done := make(chan int, 1)
	go func() {
		code, _ := supervisor.Run(supervisor.Options{
			Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
		})
		done <- code
	}()

	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sawPrompt := false
	sawExit := false
	var exitStatus string
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) && !sawExit {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		f, err := wire.Read(conn)
		if err != nil {
			break
		}
		switch f.Type {
		case wire.TypeOutput:
			if bytes.Contains(f.Payload, []byte("XANAXPROBE")) {
				sawPrompt = true
				// Now that we've seen output, request a kill.
				wire.WriteJSON(conn, wire.TypeKill, struct{}{})
			}
		case wire.TypeExit:
			var e wire.Exit
			f.DecodeJSON(&e)
			exitStatus = e.Status
			sawExit = true
		}
	}

	if !sawPrompt {
		t.Error("never saw the initial prompt echoed from the PTY")
	}
	if !sawExit {
		t.Fatal("never received an exit frame after kill")
	}
	if exitStatus != string(session.StatusCancelled) {
		t.Errorf("exit status = %q, want cancelled", exitStatus)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not return after kill")
	}

	got, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != session.StatusCancelled {
		t.Errorf("stored status = %q, want cancelled", got.Status)
	}
	if got.EndedAt == nil {
		t.Error("ended_at not recorded")
	}
}

// TestAttachFullScreenReplaysSnapshot verifies that attaching to a harness
// that entered the alternate screen buffer receives a rendered snapshot of the
// current screen (containing its content), not a raw replay of historical
// output. Diff-rendering TUIs never repaint unchanged cells, so the snapshot
// is the only way an attaching client sees the full frame.
func TestAttachFullScreenReplaysSnapshot(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "alt01", Title: "fullscreen", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	// Enter the alt screen, draw a frame, then idle.
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh",
		Args: []string{"-c", `printf '\033[?1049h'; printf 'HISTORICALFRAME'; sleep 300`}}

	go supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})

	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)
	time.Sleep(500 * time.Millisecond) // let the harness draw and the supervisor observe alt-screen

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var got []byte
	conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	for range 20 {
		f, err := wire.Read(conn)
		if err != nil {
			break
		}
		if f.Type == wire.TypeOutput {
			got = append(got, f.Payload...)
		}
	}

	if !bytes.Contains(got, []byte("\x1b[2J")) {
		t.Errorf("snapshot did not start with a clear-screen; got %.200q", got)
	}
	// The snapshot must carry the frame content (rendered from the emulator)...
	if !bytes.Contains(got, []byte("HISTORICALFRAME")) {
		t.Errorf("snapshot missing the screen content; got %.200q", got)
	}
	// ...but must not be a raw replay of the app's boot output.
	if bytes.Contains(got, []byte("\x1b[?1049h")) {
		t.Errorf("attach replayed raw output instead of a snapshot; got %.200q", got)
	}

	wire.WriteJSON(conn, wire.TypeKill, struct{}{})
	time.Sleep(300 * time.Millisecond)
}

// TestAnswersTerminalQueriesBeforeAttach verifies the supervisor replies to a
// startup DSR query so a TUI initializes correctly even though no client is
// attached yet. The fake harness sends the query, reads back the 9-byte reply,
// and echoes it (ESC shown as ^) so the test can observe it via attach.
func TestAnswersTerminalQueriesBeforeAttach(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "query01", Title: "q", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh",
		Args: []string{"-c", `printf '\033[6n'; dd bs=1 count=9 2>/dev/null | tr '\033' '^'; sleep 300`}}

	go supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})

	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)
	time.Sleep(600 * time.Millisecond) // let the query/response round-trip complete

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var got []byte
	conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	for range 20 {
		f, err := wire.Read(conn)
		if err != nil {
			break
		}
		if f.Type == wire.TypeOutput {
			got = append(got, f.Payload...)
		}
	}
	// The PTY starts at 40x120, so the DSR reply is ESC[40;120R -> "^[40;120R".
	if !bytes.Contains(got, []byte("[40;120R")) {
		t.Errorf("harness did not receive a cursor-position reply; saw %q", got)
	}
	wire.WriteJSON(conn, wire.TypeKill, struct{}{})
	time.Sleep(300 * time.Millisecond)
}

func TestNotifiesOnCompletion(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "notif01", Title: "notify me", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh", Args: []string{"-c", "exit 0"}}

	type note struct{ title, body string }
	notes := make(chan note, 4)
	supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
		Notify:   true,
		NotifyFn: func(title, body string) { notes <- note{title, body} },
	})

	select {
	case n := <-notes:
		if !bytes.Contains([]byte(n.title), []byte("Completed")) {
			t.Errorf("notification title = %q, want a Completed notice", n.title)
		}
	default:
		t.Fatal("no completion notification fired")
	}
}

// TestPeekReturnsPreviewWithoutDisturbing verifies a snapshot request gets a
// plain-text preview and does not register as a client (an attached client
// keeps receiving live output undisturbed).
func TestPeekReturnsPreviewWithoutDisturbing(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "peek01", Title: "peek", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh",
		Args: []string{"-c", `printf '\033[2;1HPEEKABLE-CONTENT'; sleep 300`}}

	go supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})
	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)
	time.Sleep(400 * time.Millisecond)

	// Peek.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	wire.WriteJSON(conn, wire.TypeSnapshotReq, wire.Resize{Rows: 8, Cols: 60})
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := wire.Read(conn)
	if err != nil {
		t.Fatalf("peek read: %v", err)
	}
	if f.Type != wire.TypeSnapshot {
		t.Fatalf("peek reply type = %d, want snapshot", f.Type)
	}
	if !bytes.Contains(f.Payload, []byte("PEEKABLE-CONTENT")) {
		t.Errorf("preview missing content; got %q", f.Payload)
	}
	if bytes.Contains(f.Payload, []byte("\x1b")) {
		t.Errorf("preview should be plain text; got %q", f.Payload)
	}

	// Clean up.
	k, _ := net.Dial("unix", sock)
	wire.WriteJSON(k, wire.TypeKill, struct{}{})
	k.Close()
	time.Sleep(200 * time.Millisecond)
}

// TestGenericStateInferenceViaPattern drives a generic harness that prints a
// "(y/n)" prompt and confirms the session flips to waiting.
func TestGenericStateInferenceViaPattern(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "infer01", Title: "infer", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{
		Adapter: config.AdapterGeneric, Command: "sh",
		Args:           []string{"-c", `sleep 0.3; printf 'Proceed? (y/n) '; sleep 300`},
		WaitingPattern: `\(y/n\)`,
	}

	go supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})
	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)

	// Poll the store until it reports waiting (the pattern matched).
	deadline := time.Now().Add(3 * time.Second)
	var status session.Status
	for time.Now().Before(deadline) {
		if got, err := st.GetSession(sess.ID); err == nil {
			status = got.Status
			if status == session.StatusWaiting {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != session.StatusWaiting {
		t.Errorf("generic session did not infer waiting; status=%q", status)
	}

	k, _ := net.Dial("unix", sock)
	wire.WriteJSON(k, wire.TypeKill, struct{}{})
	k.Close()
	time.Sleep(200 * time.Millisecond)
}

// TestGenericStateInferenceViaIdle drives a generic harness that emits once and
// then goes silent, and confirms the idle timeout flips it to waiting with an
// "idle" detail.
func TestGenericStateInferenceViaIdle(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "infer02", Title: "idle", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{
		Adapter: config.AdapterGeneric, Command: "sh",
		Args:        []string{"-c", `printf 'working...'; sleep 300`},
		IdleTimeout: 1,
	}

	go supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})
	sock := filepath.Join(paths.SocketDir, sess.ID+".sock")
	waitAlive(t, sock)

	// Poll the store until it reports waiting with an "idle" detail.
	deadline := time.Now().Add(4 * time.Second)
	var got *session.Session
	for time.Now().Before(deadline) {
		if got, err = st.GetSession(sess.ID); err == nil && got.Status == session.StatusWaiting {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got == nil || got.Status != session.StatusWaiting || got.StatusDetail != "idle" {
		t.Errorf("idle inference: status=%q detail=%q, want waiting/idle", got.Status, got.StatusDetail)
	}

	k, _ := net.Dial("unix", sock)
	wire.WriteJSON(k, wire.TypeKill, struct{}{})
	k.Close()
	time.Sleep(200 * time.Millisecond)
}

func TestSupervisorCleanCompletion(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "done01", Title: "done", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh", Args: []string{"-c", "exit 0"}}

	code, err := supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	got, _ := st.GetSession(sess.ID)
	if got.Status != session.StatusCompleted {
		t.Errorf("status = %q, want completed", got.Status)
	}
}

func TestSupervisorFailedExit(t *testing.T) {
	paths := testPaths(t)
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "fail01", Title: "fail", RepoPath: t.TempDir(),
		Harness: "generic", Status: session.StatusStarting,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "sh", Args: []string{"-c", "exit 7"}}

	code, _ := supervisor.Run(supervisor.Options{
		Session: sess, Harness: h, Paths: paths, Store: st, Logger: quietLogger(),
	})
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
	got, _ := st.GetSession(sess.ID)
	if got.Status != session.StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
}
