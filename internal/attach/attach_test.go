package attach

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"xanax/internal/wire"
)

// TestRunResetsTerminalModesOnDetach verifies the client writes the
// mode-reset sequence when the attachment ends, so harness-enabled modes
// (Kitty keyboard, mouse, ...) don't leak into whatever runs next.
func TestRunResetsTerminalModesOnDetach(t *testing.T) {
	sockDir, err := os.MkdirTemp("/tmp", "xnxat")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "s.sock")

	// Fake supervisor: accept, greet, then echo nothing and wait.
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		wire.WriteJSON(conn, wire.TypeHello, wire.Info{SessionID: "x"})
		// Drain client frames until it disconnects.
		for {
			if _, err := wire.Read(conn); err != nil {
				return
			}
		}
	}()

	// Pipes stand in for the terminal (not a TTY: raw-mode is a no-op).
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	defer outR.Close()

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64*1024)
		var all []byte
		for {
			n, err := outR.Read(buf)
			all = append(all, buf[:n]...)
			if err != nil {
				break
			}
		}
		got <- all
	}()

	resCh := make(chan Result, 1)
	go func() {
		res, _ := Run(Options{SocketPath: sock, In: inR, Out: outW})
		outW.Close() // let the collector finish
		resCh <- res
	}()

	time.Sleep(200 * time.Millisecond)
	inW.Write([]byte{0x1c}) // ctrl+\ -> detach

	select {
	case res := <-resCh:
		if res != Detached {
			t.Fatalf("Run returned %v, want Detached", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after detach")
	}
	inW.Close()

	out := <-got
	if !bytes.Contains(out, resetModes) {
		t.Errorf("terminal reset sequence not written on detach; got %q", out)
	}
}

// TestResetModesDisablesHarnessInputModes guards that every mouse/paste/focus
// mode a harness may enable through the passthrough (mouse 1000/1002/1003 with
// the SGR 1006 and urxvt 1015 encodings, bracketed paste 2004, focus 1004) is
// disabled on detach — otherwise it leaks into the dashboard. urxvt (1015) in
// particular was once missing next to SGR (1006).
func TestResetModesDisablesHarnessInputModes(t *testing.T) {
	for _, n := range []int{1000, 1002, 1003, 1006, 1015, 2004, 1004} {
		seq := []byte("\x1b[?" + strconv.Itoa(n) + "l")
		if !bytes.Contains(resetModes, seq) {
			t.Errorf("resetModes does not disable mode %d (missing %q)", n, seq)
		}
	}
}

func TestFindDetach(t *testing.T) {
	const exit = 0x1c // ctrl+\
	cases := []struct {
		name    string
		data    []byte
		wantIdx int
		wantLen int
	}{
		{"nothing", []byte("hello"), -1, 0},
		{"exit key", []byte{'a', 'b', exit}, 2, 1},
		{"left arrow CSI", []byte{'x', 0x1b, '[', 'D'}, 1, 3},
		{"left arrow SS3", []byte{0x1b, 'O', 'D'}, 0, 3},
		{"right arrow is not detach", []byte{0x1b, '[', 'C'}, -1, 0},
		{"bare esc is not detach", []byte{0x1b}, -1, 0},
		{"earliest wins: arrow before exit", []byte{0x1b, '[', 'D', exit}, 0, 3},
		{"earliest wins: exit before arrow", []byte{exit, 0x1b, '[', 'D'}, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx, length := findDetach(c.data, exit)
			if idx != c.wantIdx || length != c.wantLen {
				t.Errorf("findDetach(%v) = (%d,%d), want (%d,%d)", c.data, idx, length, c.wantIdx, c.wantLen)
			}
		})
	}
}
