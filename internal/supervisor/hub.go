package supervisor

import (
	"net"
	"sync"
	"time"

	"github.com/LeJamon/rvr/internal/ringbuf"
	"github.com/LeJamon/rvr/internal/wire"
)

const (
	clientQueueDepth     = 256
	outputChunkSize      = 32 * 1024
	exitFlushTimeout     = 2 * time.Second
	alternateScreenEnter = "\x1b[?1049h"
)

// client is one attached connection. A dedicated writer goroutine drains out,
// so broadcasters never block on a slow socket.
type client struct {
	conn net.Conn
	out  chan wire.Frame
	done chan struct{}
	once sync.Once
}

func newClient(conn net.Conn) *client {
	return &client{
		conn: conn,
		out:  make(chan wire.Frame, clientQueueDepth),
		done: make(chan struct{}),
	}
}

// enqueue queues a frame without blocking. It returns false if the client's
// queue is full (a slow consumer), signaling the caller to drop it.
func (cl *client) enqueue(f wire.Frame) bool {
	select {
	case cl.out <- f:
		return true
	case <-cl.done:
		return false
	default:
		return false
	}
}

func (cl *client) close() {
	cl.once.Do(func() {
		close(cl.done)
		cl.conn.Close()
	})
}

func (cl *client) writeLoop() {
	defer cl.close()
	for {
		select {
		case f := <-cl.out:
			if err := wire.Write(cl.conn, f.Type, f.Payload); err != nil {
				return
			}
			if f.Type == wire.TypeExit {
				return
			}
		case <-cl.done:
			return
		}
	}
}

// hub fans PTY output and state frames out to attached clients. The screen
// emulator, ring write, and every broadcast happen under mu so a newly
// registered client's snapshot is consistent with the live stream (no gaps,
// no duplication).
type hub struct {
	mu              sync.Mutex
	clients         map[*client]struct{}
	ring            *ringbuf.Ring
	screen          *screen
	modes           map[int]bool // sticky DEC modes the harness enabled (mouse, ...)
	alternateScreen bool
	info            wire.Info
	state           wire.State
	exit            *wire.Exit
}

func newHub(ring *ringbuf.Ring, scr *screen, info wire.Info) *hub {
	return &hub{
		clients: make(map[*client]struct{}),
		ring:    ring,
		screen:  scr,
		modes:   make(map[int]bool),
		info:    info,
		state:   wire.State{Status: info.Status, Detail: info.Detail},
	}
}

// broadcastOutput feeds the screen emulator, records PTY bytes and sticky
// terminal modes, and forwards the bytes live.
func (h *hub) broadcastOutput(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.alternateScreen = updateAltScreen(h.alternateScreen, p)
	h.screen.write(p)
	h.ring.Write(p)
	observeModes(h.modes, p)
	for _, chunk := range chunkBytes(p, outputChunkSize) {
		h.fanout(wire.Frame{Type: wire.TypeOutput, Payload: chunk})
	}
}

// resizeScreen keeps the emulator's grid in sync with the PTY, serialized
// against writes so snapshots stay coherent.
func (h *hub) resizeScreen(cols, rows int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.screen.resize(cols, rows)
}

// broadcastState updates the latest state and forwards it.
func (h *hub) broadcastState(s wire.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = s
	h.fanout(jsonFrame(wire.TypeState, s))
}

// broadcastExit forwards the terminal outcome in queue order and waits for
// each writer to flush it before the supervisor process exits. A single
// absolute deadline bounds the total shutdown delay for slow clients.
func (h *hub) broadcastExit(e wire.Exit) {
	h.broadcastExitWithin(e, exitFlushTimeout)
}

func (h *hub) broadcastExitWithin(e wire.Exit, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	frame := jsonFrame(wire.TypeExit, e)

	h.mu.Lock()
	terminal := e
	h.exit = &terminal
	var flushing []*client
	for cl := range h.clients {
		delete(h.clients, cl)
		_ = cl.conn.SetWriteDeadline(deadline)
		if cl.enqueue(frame) {
			flushing = append(flushing, cl)
		} else {
			cl.close()
		}
	}
	h.mu.Unlock()

	for _, cl := range flushing {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			cl.close()
			continue
		}
		timer := time.NewTimer(remaining)
		select {
		case <-cl.done:
			timer.Stop()
		case <-timer.C:
			cl.close()
		}
	}
}

// register admits a client: greet, prime the screen, send current state, then
// start receiving live frames. Held under mu so no output is missed and the
// primer exactly matches the point where the live stream resumes.
//
// For a line-based harness the scrollback ring is replayed (history matters).
// For a full-screen TUI (detected alt-screen or configured full_screen) the ring
// holds interleaved cursor-addressed diff frames that garble when replayed, and
// the app will not repaint unchanged cells, so the client receives an exact
// snapshot of the emulator's screen. When the harness is actually using the
// alternate screen, restore that mode before painting the rendered snapshot;
// the snapshot intentionally contains rendered state rather than raw mode
// transitions. Config-only snapshot replay stays on the main screen so inline
// harnesses retain native terminal scrollback. Alternate-screen state is
// tracked under the same lock as the emulator so the mode and snapshot cannot
// describe different points in the output stream.
func (h *hub) register(cl *client, configuredFullScreen bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cl.enqueue(jsonFrame(wire.TypeHello, h.info))
	primer := h.ring.Snapshot()
	if h.alternateScreen || configuredFullScreen {
		primer = h.screen.snapshot()
		if h.alternateScreen {
			primer = append([]byte(alternateScreenEnter), primer...)
		}
	}
	// Re-enable sticky modes (mouse, bracketed paste, focus) the harness set —
	// the previous detach reset them on the client's terminal.
	primer = append(primer, modeSequence(h.modes)...)
	for _, chunk := range chunkBytes(primer, outputChunkSize) {
		cl.enqueue(wire.Frame{Type: wire.TypeOutput, Payload: chunk})
	}
	cl.enqueue(jsonFrame(wire.TypeState, h.state))
	if h.exit != nil {
		_ = cl.conn.SetWriteDeadline(time.Now().Add(exitFlushTimeout))
		if !cl.enqueue(jsonFrame(wire.TypeExit, *h.exit)) {
			cl.close()
		}
		return
	}
	h.clients[cl] = struct{}{}
}

func (h *hub) remove(cl *client) {
	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
	cl.close()
}

func (h *hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// preview renders a plain-text preview of the current screen (peek). The VT
// emulator has its own lock, so this is safe without holding h.mu.
func (h *hub) preview(maxRows, maxCols int) string {
	if h.screen == nil {
		return ""
	}
	return h.screen.previewText(maxRows, maxCols)
}

// fanout must be called with mu held.
func (h *hub) fanout(f wire.Frame) {
	for cl := range h.clients {
		if !cl.enqueue(f) {
			delete(h.clients, cl)
			cl.close()
		}
	}
}

func jsonFrame(t wire.Type, v any) wire.Frame {
	// Our control payloads always marshal; ignore the impossible error.
	f, _ := wire.MarshalFrame(t, v)
	return f
}

func chunkBytes(p []byte, size int) [][]byte {
	if len(p) == 0 {
		return nil
	}
	var out [][]byte
	for len(p) > size {
		out = append(out, p[:size])
		p = p[size:]
	}
	return append(out, p)
}
