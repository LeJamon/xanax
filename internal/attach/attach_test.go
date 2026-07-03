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
		// Kitty keyboard protocol: harnesses like codex push CSI > u, so the
		// Left arrow arrives as a parameterized CSI. Unmodified presses detach.
		{"kitty report-all-keys", []byte{0x1b, '[', '1', 'D'}, 0, 4},
		{"kitty unmodified press", []byte{0x1b, '[', '1', ';', '1', 'D'}, 0, 6},
		{"kitty explicit press event", []byte{0x1b, '[', '1', ';', '1', ':', '1', 'D'}, 0, 8},
		{"kitty press after text", append([]byte("x"), 0x1b, '[', '1', ';', '1', 'D'), 1, 6},
		// Repeat and release events are not presses, so they must not detach: a
		// modifier released a beat before Left reports the Left release with no
		// modifiers (ESC[1;1:3D), which must not eject the user mid-edit.
		{"kitty unmodified release is not detach", []byte{0x1b, '[', '1', ';', '1', ':', '3', 'D'}, -1, 0},
		{"kitty unmodified repeat is not detach", []byte{0x1b, '[', '1', ';', '1', ':', '2', 'D'}, -1, 0},
		// A non-Left key code sharing the final 'D' (e.g. ANSI cursor-back-N,
		// common in pasted terminal output) is not the Left arrow.
		{"csi cursor-back-5 is not detach", []byte{0x1b, '[', '5', 'D'}, -1, 0},
		{"csi cursor-back-3 is not detach", []byte{0x1b, '[', '3', 'D'}, -1, 0},
		{"csi other keycode is not detach", []byte{0x1b, '[', '5', '7', '4', '4', '4', ';', '1', 'D'}, -1, 0},
		// Coalesced press+release from one Left tap (report-event-types on):
		// the press matches first. This is the case the old exact-match missed.
		{"kitty press then release coalesced", []byte{0x1b, '[', '1', ';', '1', 'D', 0x1b, '[', '1', ';', '1', ':', '3', 'D'}, 0, 6},
		// Lock states (num_lock=128, caps_lock=64) are not real modifiers, so a
		// plain Left with Num/Caps Lock on still detaches (ESC[1;129D / ESC[1;65D).
		{"kitty left with num lock detaches", []byte{0x1b, '[', '1', ';', '1', '2', '9', 'D'}, 0, 8},
		{"kitty left with caps lock detaches", []byte{0x1b, '[', '1', ';', '6', '5', 'D'}, 0, 7},
		{"kitty ctrl+left with num lock is not detach", []byte{0x1b, '[', '1', ';', '1', '3', '3', 'D'}, -1, 0},
		// Modified Left passes through to the harness (word nav / selection).
		{"kitty ctrl+left is not detach", []byte{0x1b, '[', '1', ';', '5', 'D'}, -1, 0},
		{"kitty alt+left is not detach", []byte{0x1b, '[', '1', ';', '3', 'D'}, -1, 0},
		{"kitty shift+left is not detach", []byte{0x1b, '[', '1', ';', '2', 'D'}, -1, 0},
		{"kitty ctrl+right is not detach", []byte{0x1b, '[', '1', ';', '5', 'C'}, -1, 0},
		// A modified arrow still lets a following exit key detach.
		{"modified arrow then exit", []byte{0x1b, '[', '1', ';', '5', 'D', exit}, 6, 1},
		// Partial sequence at a read boundary: no match; the prefix is forwarded
		// and the detach is missed (an accepted limitation).
		{"partial kitty csi", []byte{0x1b, '[', '1', ';'}, -1, 0},
		// Exit key (ctrl+\) under the Kitty protocol: the disambiguate flag
		// encodes it as a CSI-u sequence (ESC[92;5u, codepoint 92 = '\', mod 5 =
		// 1+ctrl) instead of the raw 0x1c byte, so a raw scan alone would miss it.
		{"kitty exit key csi-u", []byte{0x1b, '[', '9', '2', ';', '5', 'u'}, 0, 7},
		{"kitty exit key csi-u after text", append([]byte("hi"), 0x1b, '[', '9', '2', ';', '5', 'u'), 2, 7},
		{"kitty exit key csi-u explicit press", []byte{0x1b, '[', '9', '2', ';', '5', ':', '1', 'u'}, 0, 9},
		{"kitty exit key csi-u release is not detach", []byte{0x1b, '[', '9', '2', ';', '5', ':', '3', 'u'}, -1, 0},
		// Ctrl held alongside num lock (mod 133 = 1+ctrl+num_lock) still detaches.
		{"kitty exit key csi-u with num lock", []byte{0x1b, '[', '9', '2', ';', '1', '3', '3', 'u'}, 0, 9},
		// Backslash without ctrl (mod 1) is a literal key, not the exit chord.
		{"kitty backslash without ctrl is not detach", []byte{0x1b, '[', '9', '2', ';', '1', 'u'}, -1, 0},
		{"kitty backslash report-all-keys is not detach", []byte{0x1b, '[', '9', '2', 'u'}, -1, 0},
		// A different ctrl chord (ctrl+a = ESC[97;5u) is not the exit ctrl+\.
		{"kitty other ctrl chord is not detach", []byte{0x1b, '[', '9', '7', ';', '5', 'u'}, -1, 0},
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
