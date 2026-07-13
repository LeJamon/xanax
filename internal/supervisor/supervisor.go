// Package supervisor owns one agent session: it runs the harness in a PTY,
// records and replays output, tracks state, and serves a unix socket so
// clients can attach and detach without ending the session (SPEC.md §3, §4).
package supervisor

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/LeJamon/rvr/internal/adapter"
	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/notify"
	"github.com/LeJamon/rvr/internal/ringbuf"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/wire"
)

const (
	ringSize      = 256 * 1024
	rawLogCap     = 10 * 1024 * 1024
	killGrace     = 5 * time.Second
	refPollPeriod = 2 * time.Second
	resumeOwnWait = 5 * time.Second
	resumeOwnPoll = 25 * time.Millisecond
)

// ErrAlreadySupervised means another process owns this session's supervisor
// lease. The losing process must exit without changing the session record.
var ErrAlreadySupervised = errors.New("session already has a supervisor")

// Options configures a supervisor run.
type Options struct {
	Session *session.Session
	Harness config.Harness
	Paths   config.Paths
	Store   *store.Store
	Logger  *slog.Logger
	Resume  bool
	Notify  bool
	// NotifyFn delivers a desktop notification; nil defaults to notify.Send.
	// Injectable for tests.
	NotifyFn func(title, body string)
}

// Supervisor is a single session's owner process state.
type Supervisor struct {
	opts     Options
	log      *slog.Logger
	adapter  adapter.Adapter
	notifyFn func(title, body string)

	ptmx *os.File
	cmd  *exec.Cmd

	ring   *ringbuf.Ring
	rawLog *cappedFile
	hub    *hub

	listener net.Listener
	owner    *Lease
	clientWG sync.WaitGroup

	sizeMu   sync.Mutex
	curRows  uint16
	curCols  uint16
	killedBy atomic.Bool

	// Non-terminal status tracking, shared by the native state watcher and the
	// generic inference path (only one is active per session).
	statusMu  sync.Mutex
	curStatus session.Status
	curDetail string

	// Generic state inference (active only when the adapter has no state
	// channel and the harness configured it).
	infer *inferer
	// altScreen is true when the harness has switched to the alternate screen
	// buffer (i.e. it is a full-screen TUI). Attaching clients then get a clean
	// repaint instead of a replay of stale frames (SPEC.md §4).
	altScreen atomic.Bool
	exited    chan struct{}

	// stateInfo is written only by watchState and read by run after wg.Wait,
	// so the happens-before from wg makes extra locking unnecessary.
	stateInfo stateTracker
}

// stateTracker accumulates what classifyExit needs from the state stream.
type stateTracker struct {
	had  bool
	saw  bool
	last adapter.StateKind
}

// Run launches the harness and blocks until it exits, returning the process
// exit code. It records all lifecycle transitions to the store.
func Run(opts Options) (int, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Supervisor{
		opts:     opts,
		log:      opts.Logger.With("session", opts.Session.ID),
		notifyFn: opts.NotifyFn,
		exited:   make(chan struct{}),
	}
	if s.notifyFn == nil {
		s.notifyFn = func(title, body string) { _ = notify.Send(title, body) }
	}
	return s.run()
}

func (s *Supervisor) run() (int, error) {
	sess := s.opts.Session
	if err := s.acquireOwnership(); err != nil {
		if errors.Is(err, ErrAlreadySupervised) {
			return 0, err
		}
		s.fail("acquire supervisor ownership failed: " + err.Error())
		return 1, err
	}
	defer s.releaseOwnership()

	// A supervisor process may have been spawned just before the session was
	// removed. Removal holds this same lease through DeleteSession, so reloading
	// after ownership is acquired closes that scheduling window: a stale child
	// observes the missing row and exits before it creates a socket or harness.
	fresh, err := s.opts.Store.GetSession(sess.ID)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 1, fmt.Errorf("reload session after acquiring ownership: %w", err)
	}
	// A duplicate resume child can outwait a fast first lifecycle. Never turn
	// that completed lifecycle into another run merely because its lease became
	// available before this process's bounded ownership wait expired.
	if s.opts.Resume && fresh.Status.Terminal() {
		return 0, nil
	}
	sess = fresh
	s.opts.Session = fresh

	a, err := adapter.New(sess, s.opts.Harness, adapter.Deps{Paths: s.opts.Paths, Logger: s.log})
	if err != nil {
		s.fail("adapter init failed: " + err.Error())
		return 1, err
	}
	s.adapter = a
	defer a.Close()

	if err := s.openRawLog(); err != nil {
		s.fail("open raw log failed: " + err.Error())
		return 1, err
	}
	defer s.rawLog.Close()

	if err := s.listen(); err != nil {
		s.fail("socket listen failed: " + err.Error())
		return 1, err
	}
	defer s.cleanupSocket()

	spec, err := a.Launch(s.opts.Resume)
	if err != nil {
		s.fail("launch spec failed: " + err.Error())
		return 1, err
	}

	if err := s.startPTY(spec); err != nil {
		s.fail("start harness failed: " + err.Error())
		return 1, err
	}

	s.ring = ringbuf.New(ringSize)
	s.hub = newHub(s.ring, newScreen(int(s.curCols), int(s.curRows)), wire.Info{
		SessionID: sess.ID,
		Title:     sess.Title,
		Harness:   sess.Harness,
		Status:    string(session.StatusRunning),
	})

	if err := s.markRunning(); err != nil {
		s.stopUnregisteredHarness()
		s.fail("mark running failed: " + err.Error())
		return 1, err
	}
	// Generic state inference: only when the harness exposes no native state
	// channel and configured a heuristic.
	if a.States() == nil {
		s.infer = newInferer(s.opts.Harness, s.log)
	}
	if err := a.AfterStart(s.ptmx); err != nil {
		s.log.Warn("adapter AfterStart failed", "err", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.pumpOutput() }()
	go func() { defer wg.Done(); s.acceptClients() }()
	go func() { defer wg.Done(); s.watchState() }()
	go s.pollSessionRef() // stops when exited closes
	// The idle checker is part of wg so finish() (after wg.Wait) is guaranteed
	// to run after any in-flight idle transition — a late idle tick can never
	// overwrite the terminal status.
	if s.infer != nil && s.infer.idle > 0 {
		wg.Add(1)
		go func() { defer wg.Done(); s.idleLoop() }()
	}

	exitCode := s.wait()
	close(s.exited)

	a.Close() // stops the state channel; watchState returns
	s.listener.Close()
	wg.Wait()

	final := classifyExit(exitInfo{
		killedByUs:      s.killedBy.Load(),
		exitCode:        exitCode,
		hadStateChannel: s.stateInfo.had,
		sawState:        s.stateInfo.saw,
		lastKind:        s.stateInfo.last,
	})
	s.finish(final, exitCode)
	s.clientWG.Wait()
	return exitCode, nil
}

func (s *Supervisor) startPTY(spec adapter.LaunchSpec) error {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	env := spec.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = withStableTerm(env)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return err
	}
	s.ptmx = ptmx
	s.cmd = cmd
	s.curRows, s.curCols = 40, 120
	s.log.Info("harness started", "pid", cmd.Process.Pid, "path", spec.Path)
	return nil
}

// pumpOutput copies PTY output into the screen emulator, ring, and raw log,
// and broadcasts it live. Chunks are normalized to sequence-safe boundaries
// first (splitSafe) so a client attaching between chunks never starts
// mid-escape-sequence, and the alt-screen tracker sees whole sequences.
func (s *Supervisor) pumpOutput() {
	buf := make([]byte, 32*1024)
	var carry []byte
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := append(carry, buf[:n]...)
			emit, rest := splitSafe(data)
			carry = append([]byte(nil), rest...)
			if len(emit) > 0 {
				s.answerTerminalQueries(emit)
				s.altScreen.Store(updateAltScreen(s.altScreen.Load(), emit))
				s.rawLog.Write(emit)
				s.hub.broadcastOutput(emit)
				if s.infer != nil {
					s.inferObserve(emit)
				}
			}
		}
		if err != nil {
			return // PTY closed: harness exited
		}
	}
}

// alt-screen enter/leave control sequences (xterm private modes 1049/1047/47).
var (
	altEnterSeqs = [][]byte{[]byte("\x1b[?1049h"), []byte("\x1b[?1047h"), []byte("\x1b[?47h")}
	altLeaveSeqs = [][]byte{[]byte("\x1b[?1049l"), []byte("\x1b[?1047l"), []byte("\x1b[?47l")}
)

// updateAltScreen returns the alternate-screen state after observing chunk. It
// uses the last enter/leave marker in the chunk so a chunk containing both
// resolves to whichever came last.
func updateAltScreen(cur bool, chunk []byte) bool {
	lastEnter, lastLeave := -1, -1
	for _, s := range altEnterSeqs {
		if i := bytes.LastIndex(chunk, s); i > lastEnter {
			lastEnter = i
		}
	}
	for _, s := range altLeaveSeqs {
		if i := bytes.LastIndex(chunk, s); i > lastLeave {
			lastLeave = i
		}
	}
	if lastEnter < 0 && lastLeave < 0 {
		return cur
	}
	return lastEnter > lastLeave
}

// answerTerminalQueries replies to capability queries in the harness output,
// but only while no client is attached — an attached real terminal answers its
// own queries, so replying then would double up.
func (s *Supervisor) answerTerminalQueries(chunk []byte) {
	if s.hub.clientCount() > 0 {
		return
	}
	s.sizeMu.Lock()
	rows, cols := s.curRows, s.curCols
	s.sizeMu.Unlock()
	if resp := terminalQueryResponses(chunk, rows, cols); len(resp) > 0 {
		s.ptmx.Write(resp)
	}
}

func (s *Supervisor) acceptClients() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.clientWG.Add(1)
		go func() {
			defer s.clientWG.Done()
			s.serveClient(newClient(conn))
		}()
	}
}

func (s *Supervisor) serveClient(cl *client) {
	// The first frame is a peek request (one-shot preview, no attach) or the
	// attach client's terminal size. Read it before starting the write loop so
	// a peek can reply synchronously and close. Applying the size BEFORE
	// registering makes the screen snapshot render at the client's width — a
	// wrong-width snapshot wraps every row and scrolls content away.
	cl.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	first, firstErr := wire.Read(cl.conn)
	cl.conn.SetReadDeadline(time.Time{})

	if firstErr == nil && first.Type == wire.TypeSnapshotReq {
		var r wire.Resize
		_ = first.DecodeJSON(&r)
		_ = cl.conn.SetWriteDeadline(time.Now().Add(exitFlushTimeout))
		_ = wire.Write(cl.conn, wire.TypeSnapshot, []byte(s.hub.preview(int(r.Rows), int(r.Cols))))
		cl.close() // peek is never registered as a client
		return
	}

	go cl.writeLoop()
	if firstErr == nil {
		if !s.handleClientFrame(first) {
			cl.close()
			return
		}
	}

	s.hub.register(cl, s.altScreen.Load() || s.opts.Harness.FullScreen)
	defer s.hub.remove(cl)

	for {
		f, err := wire.Read(cl.conn)
		if err != nil {
			return
		}
		if !s.handleClientFrame(f) {
			return
		}
	}
}

// handleClientFrame applies one client frame; false means the client detached.
func (s *Supervisor) handleClientFrame(f wire.Frame) bool {
	switch f.Type {
	case wire.TypeInput:
		s.ptmx.Write(f.Payload)
	case wire.TypeResize:
		var r wire.Resize
		if f.DecodeJSON(&r) == nil {
			s.resize(r.Rows, r.Cols)
		}
	case wire.TypeKill:
		s.requestKill()
	case wire.TypeDetach:
		return false
	}
	return true
}

// resize applies the client's terminal size to the emulator and the PTY. The
// emulator resizes first so the app's post-SIGWINCH repaint is parsed against
// the new grid. When the size is unchanged, the rows are nudged to force a
// SIGWINCH so the harness TUI repaints on attach (the dtach trick, SPEC.md §4).
func (s *Supervisor) resize(rows, cols uint16) {
	if rows == 0 || cols == 0 {
		return
	}
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	s.hub.resizeScreen(int(cols), int(rows))
	if rows == s.curRows && cols == s.curCols {
		_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows - 1, Cols: cols})
	}
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
	s.curRows, s.curCols = rows, cols
}

func (s *Supervisor) watchState() {
	ch := s.adapter.States()
	if ch == nil {
		return
	}
	s.stateInfo.had = true
	for ev := range ch {
		s.stateInfo.saw = true
		s.stateInfo.last = ev.Kind
		status, detail, change := mapState(ev)
		s.opts.Store.RecordEvent(s.opts.Session.ID, "state", map[string]any{
			"kind": ev.Kind.String(), "detail": ev.Message,
		})
		if change {
			s.applyStatus(status, detail)
		}
	}
}

// applyStatus records a non-terminal status transition from the native state
// watcher and notifies on an actual change. See applyStatusLocked.
func (s *Supervisor) applyStatus(status session.Status, detail string) {
	s.statusMu.Lock()
	prev, changed := s.applyStatusLocked(status, detail)
	s.statusMu.Unlock()
	if changed {
		s.raiseNotification(prev, status, detail)
	}
}

// applyStatusLocked persists a non-terminal status transition and broadcasts it
// to attached clients, holding statusMu across both so the store and the live
// wire state stay totally ordered. This is what lets the generic inferer's
// output and idle signals interleave safely: whichever acquires the lock last
// wins in the store exactly as it does in curStatus, so a stale idle tick can
// never leave the session stuck "waiting" while output flows. It reports the
// previous status and whether the status actually changed so the caller can
// notify AFTER releasing the lock (the desktop notification may block). The
// caller must hold statusMu; the notification is intentionally left to it.
func (s *Supervisor) applyStatusLocked(status session.Status, detail string) (prev session.Status, changed bool) {
	prev = s.curStatus
	if status == prev && detail == s.curDetail {
		return prev, false
	}
	s.curStatus, s.curDetail = status, detail
	if err := s.opts.Store.SetStatus(s.opts.Session.ID, status, detail); err != nil {
		s.log.Warn("set status failed", "err", err)
	}
	s.hub.broadcastState(wire.State{Status: string(status), Detail: detail})
	return prev, status != prev
}

func (s *Supervisor) pollSessionRef() {
	t := time.NewTicker(refPollPeriod)
	defer t.Stop()
	last := s.opts.Session.HarnessSessionRef
	for {
		select {
		case <-s.exited:
			s.persistRef(&last)
			return
		case <-t.C:
			s.persistRef(&last)
		}
	}
}

func (s *Supervisor) persistRef(last *string) {
	ref := s.adapter.SessionRef()
	if ref == "" || ref == *last {
		return
	}
	if err := s.opts.Store.SetSessionRef(s.opts.Session.ID, ref); err != nil {
		s.log.Warn("persist session ref failed", "err", err)
		return
	}
	*last = ref
	s.log.Info("captured harness session ref", "ref", ref)
}

func (s *Supervisor) requestKill() {
	if s.killedBy.Swap(true) {
		return // already killing
	}
	s.opts.Store.RecordEvent(s.opts.Session.ID, "killed", nil)
	pgid := s.cmd.Process.Pid
	s.log.Info("terminating harness", "pgid", pgid)
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	go func() {
		select {
		case <-s.exited:
		case <-time.After(killGrace):
			s.log.Warn("harness did not exit; sending SIGKILL")
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}()
}

func (s *Supervisor) wait() int {
	err := s.cmd.Wait()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return ws.ExitStatus()
		}
	}
	s.log.Warn("harness wait error", "err", err)
	return 1
}

// --- store transitions ---

func (s *Supervisor) markRunning() error {
	id := s.opts.Session.ID
	s.curStatus = session.StatusRunning
	if err := s.opts.Store.SetRuntime(id, os.Getpid(), s.socketPath(), session.StatusRunning); err != nil {
		return err
	}
	s.opts.Store.RecordEvent(id, "started", map[string]any{"pid": os.Getpid()})
	return nil
}

// stopUnregisteredHarness guarantees a process never outlives a failed
// SetRuntime. At this point no pump, accept, or state goroutines have started,
// so a hard process-group stop followed by Wait is the complete cleanup path.
func (s *Supervisor) stopUnregisteredHarness() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	_ = s.cmd.Wait()
}

func (s *Supervisor) finish(status session.Status, exitCode int) {
	id := s.opts.Session.ID
	if err := s.opts.Store.Finish(id, status, exitCode); err != nil {
		s.log.Warn("finish failed", "err", err)
	}
	s.opts.Store.RecordEvent(id, "exited", map[string]any{
		"status": string(status), "exit_code": exitCode,
	})
	if s.hub != nil {
		s.hub.broadcastExit(wire.Exit{Status: string(status), ExitCode: exitCode})
	}
	// Notify on terminal outcome; running is the implicit "prev" so a terminal
	// state is always a transition. Cancelled is user-initiated → stays quiet.
	s.raiseNotification(session.StatusRunning, status, s.opts.Session.StatusDetail)
	s.log.Info("session ended", "status", status, "exit_code", exitCode)
}

// fail records an early startup failure before the harness ever ran.
func (s *Supervisor) fail(msg string) {
	id := s.opts.Session.ID
	s.log.Error("supervisor startup failed", "msg", msg)
	s.opts.Store.RecordEvent(id, "error", map[string]any{"message": msg})
	_ = s.opts.Store.FinishWithDetail(id, session.StatusFailed, 1, msg)
}

// --- socket / raw log helpers ---

func (s *Supervisor) socketPath() string {
	return filepath.Join(s.opts.Paths.SocketDir, s.opts.Session.ID+".sock")
}

func (s *Supervisor) acquireOwnership() error {
	deadline := time.Now()
	if s.opts.Resume {
		// A concurrent prune may have selected the old terminal lifecycle and be
		// finishing its conditional delete. Give that short-lived lease holder
		// time to leave; normal duplicate supervisors still fail promptly.
		deadline = deadline.Add(resumeOwnWait)
	}
	for {
		lease, err := TryAcquireLease(s.socketPath())
		if err == nil {
			s.owner = lease
			return nil
		}
		if !errors.Is(err, ErrAlreadySupervised) {
			return err
		}
		if !s.opts.Resume || !time.Now().Before(deadline) {
			return fmt.Errorf("%w: %s", ErrAlreadySupervised, s.opts.Session.ID)
		}
		time.Sleep(resumeOwnPoll)
	}
}

func (s *Supervisor) releaseOwnership() {
	if s.owner == nil {
		return
	}
	s.owner.Release()
	s.owner = nil
}

func (s *Supervisor) listen() error {
	path := s.socketPath()
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	s.listener = l
	return nil
}

func (s *Supervisor) cleanupSocket() {
	_ = os.Remove(s.socketPath())
}

func (s *Supervisor) openRawLog() error {
	if err := os.MkdirAll(s.opts.Paths.LogsDir, 0o700); err != nil {
		return err
	}
	f, err := newCappedFile(filepath.Join(s.opts.Paths.LogsDir, s.opts.Session.ID+".raw"), rawLogCap)
	if err != nil {
		return err
	}
	s.rawLog = f
	return nil
}
