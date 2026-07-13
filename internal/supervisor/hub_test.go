package supervisor

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/LeJamon/rvr/internal/ringbuf"
	"github.com/LeJamon/rvr/internal/wire"
)

func TestBroadcastExitFlushesQueuedFramesInOrder(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	cl := newClient(server)
	go cl.writeLoop()
	h := &hub{clients: map[*client]struct{}{cl: {}}}
	if !cl.enqueue(wire.Frame{Type: wire.TypeOutput, Payload: []byte("before exit")}) {
		t.Fatal("enqueue output failed")
	}

	flushed := make(chan struct{})
	go func() {
		h.broadcastExitWithin(wire.Exit{Status: "completed", ExitCode: 0}, time.Second)
		close(flushed)
	}()
	select {
	case <-flushed:
		t.Fatal("broadcast returned before the peer read queued frames")
	case <-time.After(20 * time.Millisecond):
	}

	first, err := wire.Read(peer)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if first.Type != wire.TypeOutput || string(first.Payload) != "before exit" {
		t.Fatalf("first frame = (%v, %q), want queued output", first.Type, first.Payload)
	}
	last, err := wire.Read(peer)
	if err != nil {
		t.Fatalf("read exit: %v", err)
	}
	if last.Type != wire.TypeExit {
		t.Fatalf("last frame type = %v, want exit", last.Type)
	}
	var exit wire.Exit
	if err := last.DecodeJSON(&exit); err != nil {
		t.Fatalf("decode exit: %v", err)
	}
	if exit.Status != "completed" || exit.ExitCode != 0 {
		t.Fatalf("exit = %+v", exit)
	}
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not finish after exit was read")
	}
	if _, err := wire.Read(peer); !errors.Is(err, io.EOF) {
		t.Fatalf("post-exit read error = %v, want EOF", err)
	}
}

func TestBroadcastExitBoundsStalledClient(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	cl := newClient(server)
	go cl.writeLoop()
	h := &hub{clients: map[*client]struct{}{cl: {}}}

	started := time.Now()
	h.broadcastExitWithin(wire.Exit{Status: "failed", ExitCode: 1}, 25*time.Millisecond)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("stalled exit flush took %s", elapsed)
	}
	select {
	case <-cl.done:
	default:
		t.Fatal("stalled client was not closed")
	}
}

func TestRegisterAfterExitReceivesTerminalFrame(t *testing.T) {
	h := newHub(ringbuf.New(1024), newScreen(80, 24), wire.Info{SessionID: "late"})
	h.broadcastExitWithin(wire.Exit{Status: "completed"}, 25*time.Millisecond)

	server, peer := net.Pipe()
	defer peer.Close()
	cl := newClient(server)
	go cl.writeLoop()
	h.register(cl, false)
	for _, want := range []wire.Type{wire.TypeHello, wire.TypeState, wire.TypeExit} {
		frame, err := wire.Read(peer)
		if err != nil {
			t.Fatalf("read %v: %v", want, err)
		}
		if frame.Type != want {
			t.Fatalf("frame type = %v, want %v", frame.Type, want)
		}
	}
	if _, err := wire.Read(peer); !errors.Is(err, io.EOF) {
		t.Fatalf("post-exit read error = %v, want EOF", err)
	}
}
