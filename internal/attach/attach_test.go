package attach

import (
	"bytes"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/LeJamon/rvr/internal/wire"
)

// TestRunResetsTerminalModesOnDetach verifies the client writes the
// mode-reset sequence when the attachment ends, so harness-enabled modes
// (Kitty keyboard, mouse, ...) don't leak into whatever runs next.
func TestRunResetsTerminalModesOnDetach(t *testing.T) {
	out, res := runAttachTest(t)
	if res != Detached {
		t.Fatalf("Run returned %v, want Detached", res)
	}

	if !bytes.Contains(out, resetModes) {
		t.Errorf("terminal reset sequence not written on detach; got %q", out)
	}
}

// TestRunStartsOnScrollableCleanScreen verifies a new attachment clears away
// the visible terminal contents without entering the alternate screen. Keeping
// the main screen active lets inline harnesses retain native scrollback.
func TestRunStartsOnScrollableCleanScreen(t *testing.T) {
	out, res := runAttachTest(t)
	if res != Detached {
		t.Fatalf("Run returned %v, want Detached", res)
	}
	if !bytes.HasPrefix(out, enterSessionScreen) {
		t.Fatalf("attach did not start with clean session screen; got %q", out)
	}
	if !bytes.Contains(enterSessionScreen, []byte("\x1b[2J")) {
		t.Errorf("clean screen primer is missing clear-screen; got %q", enterSessionScreen)
	}
	if bytes.Contains(enterSessionScreen, []byte("\x1b[?1049h")) {
		t.Errorf("clean screen primer entered alternate screen and disabled native scrollback; got %q", enterSessionScreen)
	}
	if bytes.Contains(enterSessionScreen, []byte("\x1b[?25l")) {
		t.Fatal("clean screen primer must not hide the cursor; line-based sessions may not show it again")
	}
}

func runAttachTest(t *testing.T) ([]byte, Result) {
	t.Helper()

	client, server := net.Pipe()
	go func() {
		defer server.Close()
		wire.WriteJSON(server, wire.TypeHello, wire.Info{SessionID: "x"})
		for {
			if _, err := wire.Read(server); err != nil {
				return
			}
		}
	}()

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
		res, _ := runConnected(Options{In: inR, Out: outW}, client)
		outW.Close()
		resCh <- res
	}()

	time.Sleep(200 * time.Millisecond)
	inW.Write([]byte{defaultExitKey}) // ctrl+q -> detach

	var res Result
	select {
	case res = <-resCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after detach")
	}
	inW.Close()
	return <-got, res
}

func TestRunForwardsLeftArrow(t *testing.T) {
	cases := []struct {
		name string
		left []byte
	}{
		{"legacy CSI", []byte{0x1b, '[', 'D'}},
		{"application cursor SS3", []byte{0x1b, 'O', 'D'}},
		{"Kitty report all keys", []byte{0x1b, '[', '1', 'D'}},
		{"Kitty explicit press", []byte{0x1b, '[', '1', ';', '1', ':', '1', 'D'}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()

			inR, inW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer inR.Close()
			defer inW.Close()

			out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()

			framesCh := make(chan []wire.Frame, 1)
			go func() {
				var frames []wire.Frame
				for {
					frame, err := wire.Read(server)
					if err != nil {
						framesCh <- frames
						return
					}
					frames = append(frames, frame)
					if frame.Type == wire.TypeDetach {
						framesCh <- frames
						return
					}
				}
			}()

			resCh := make(chan Result, 1)
			go func() {
				res, _ := runConnected(Options{In: inR, Out: out}, client)
				resCh <- res
			}()

			input := append(append([]byte(nil), tc.left...), defaultExitKey)
			if _, err := inW.Write(input); err != nil {
				t.Fatal(err)
			}

			var res Result
			select {
			case res = <-resCh:
			case <-time.After(3 * time.Second):
				t.Fatal("attach did not return after exit key")
			}
			if res != Detached {
				t.Fatalf("runConnected returned %v, want Detached", res)
			}

			var forwarded []byte
			var detached bool
			for _, frame := range <-framesCh {
				switch frame.Type {
				case wire.TypeInput:
					forwarded = append(forwarded, frame.Payload...)
				case wire.TypeDetach:
					detached = true
				}
			}
			if !bytes.Equal(forwarded, tc.left) {
				t.Errorf("forwarded input = %q, want Left sequence %q", forwarded, tc.left)
			}
			if !detached {
				t.Error("configured exit key did not send detach after forwarding Left")
			}
		})
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
	const exit = defaultExitKey // ctrl+q
	cases := []struct {
		name    string
		data    []byte
		wantIdx int
		wantLen int
	}{
		{"nothing", []byte("hello"), -1, 0},
		{"exit key", []byte{'a', 'b', exit}, 2, 1},
		{"left arrow CSI passes through", []byte{'x', 0x1b, '[', 'D'}, -1, 0},
		{"left arrow SS3 passes through", []byte{0x1b, 'O', 'D'}, -1, 0},
		{"right arrow is not detach", []byte{0x1b, '[', 'C'}, -1, 0},
		{"bare esc is not detach", []byte{0x1b}, -1, 0},
		{"left before exit passes through", []byte{0x1b, '[', 'D', exit}, 3, 1},
		{"earliest wins: exit before arrow", []byte{exit, 0x1b, '[', 'D'}, 0, 1},
		// Kitty keyboard protocol: harnesses like codex push CSI > u, so the
		// Left arrow arrives as a parameterized CSI. Every form passes through.
		{"kitty report-all-keys left passes through", []byte{0x1b, '[', '1', 'D'}, -1, 0},
		{"kitty unmodified left passes through", []byte{0x1b, '[', '1', ';', '1', 'D'}, -1, 0},
		{"kitty explicit left press passes through", []byte{0x1b, '[', '1', ';', '1', ':', '1', 'D'}, -1, 0},
		{"kitty left after text passes through", append([]byte("x"), 0x1b, '[', '1', ';', '1', 'D'), -1, 0},
		{"kitty unmodified release is not detach", []byte{0x1b, '[', '1', ';', '1', ':', '3', 'D'}, -1, 0},
		{"kitty unmodified repeat is not detach", []byte{0x1b, '[', '1', ';', '1', ':', '2', 'D'}, -1, 0},
		// A non-Left key code sharing the final 'D' (e.g. ANSI cursor-back-N,
		// common in pasted terminal output) is not the Left arrow.
		{"csi cursor-back-5 is not detach", []byte{0x1b, '[', '5', 'D'}, -1, 0},
		{"csi cursor-back-3 is not detach", []byte{0x1b, '[', '3', 'D'}, -1, 0},
		{"csi other keycode is not detach", []byte{0x1b, '[', '5', '7', '4', '4', '4', ';', '1', 'D'}, -1, 0},
		{"kitty press then release passes through", []byte{0x1b, '[', '1', ';', '1', 'D', 0x1b, '[', '1', ';', '1', ':', '3', 'D'}, -1, 0},
		{"kitty left with num lock passes through", []byte{0x1b, '[', '1', ';', '1', '2', '9', 'D'}, -1, 0},
		{"kitty left with caps lock passes through", []byte{0x1b, '[', '1', ';', '6', '5', 'D'}, -1, 0},
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
		// Exit key (ctrl+q) under the Kitty protocol: the disambiguate flag
		// encodes it as a CSI-u sequence (ESC[113;5u, codepoint 113 = 'q', mod 5 =
		// 1+ctrl) instead of the raw 0x11 byte, so a raw scan alone would miss it.
		{"kitty exit key csi-u", []byte("\x1b[113;5u"), 0, 8},
		{"kitty exit key csi-u after text", append([]byte("hi"), []byte("\x1b[113;5u")...), 2, 8},
		{"kitty exit key csi-u explicit press", []byte("\x1b[113;5:1u"), 0, 10},
		{"kitty exit key csi-u release is not detach", []byte("\x1b[113;5:3u"), -1, 0},
		{"kitty exit key csi-u repeat is not detach", []byte("\x1b[113;5:2u"), -1, 0},
		// Ctrl held alongside num lock (mod 133 = 1+ctrl+num_lock) still detaches.
		{"kitty exit key csi-u with num lock", []byte("\x1b[113;133u"), 0, 10},
		{"kitty exit key csi-u with caps lock", []byte("\x1b[113;69u"), 0, 9},
		// Alternate-key reporting adds the base-layout key. On AZERTY the Q key
		// can therefore arrive with q as the primary codepoint and a as its base.
		{"kitty exit key csi-u with AZERTY base key", []byte("\x1b[113::97;5u"), 0, 12},
		{"kitty q without ctrl is not detach", []byte("\x1b[113;1u"), -1, 0},
		{"kitty q report-all-keys is not detach", []byte("\x1b[113u"), -1, 0},
		{"kitty ctrl+shift+q is not detach", []byte("\x1b[113:81;6u"), -1, 0},
		{"kitty ctrl+alt+q is not detach", []byte("\x1b[113;7u"), -1, 0},
		{"kitty AZERTY ctrl+a is not ctrl+q", []byte("\x1b[97::113;5u"), -1, 0},
		{"kitty overflowing codepoint is not detach", []byte("\x1b[999999999999999999999;5u"), -1, 0},
		{"kitty ctrl+1 does not alias ctrl+q", []byte("\x1b[49;5u"), -1, 0},
		{"kitty malformed alternate key is not detach", []byte("\x1b[113:;5u"), -1, 0},
		{"old raw default is not detach", []byte{0x1c}, -1, 0},
		{"old kitty default is not detach", []byte("\x1b[92;5u"), -1, 0},
		// A different ctrl chord (ctrl+a = ESC[97;5u) is not ctrl+q.
		{"kitty other ctrl chord is not detach", []byte("\x1b[97;5u"), -1, 0},
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

func TestFindDetachPreservesConfiguredCtrlBackslash(t *testing.T) {
	const ctrlBackslash = 0x1c
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"raw", []byte{ctrlBackslash}, 1},
		{"kitty", []byte("\x1b[92;5u"), 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, length := findDetach(tc.data, ctrlBackslash)
			if idx != 0 || length != tc.want {
				t.Fatalf("findDetach(%v, ctrl+\\) = (%d, %d), want (0, %d)", tc.data, idx, length, tc.want)
			}
		})
	}
}

func TestFindDetachHandlesConfiguredShiftedControlKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     byte
		data    string
		wantLen int
	}{
		{"ctrl+caret", 0x1e, "\x1b[54:94;6u", 10},
		{"ctrl+caret without alternate metadata", 0x1e, "\x1b[54;6u", 7},
		{"ctrl+caret with conflicting metadata", 0x1e, "\x1b[54:38;6u", 0},
		{"ctrl+underscore", 0x1f, "\x1b[45:95;6u", 10},
		{"ctrl+underscore without alternate metadata", 0x1f, "\x1b[45;6u", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, length := findDetach([]byte(tc.data), tc.key)
			if tc.wantLen == 0 {
				if idx != -1 || length != 0 {
					t.Fatalf("findDetach(%q) = (%d, %d), want no match", tc.data, idx, length)
				}
				return
			}
			if idx != 0 || length != tc.wantLen {
				t.Fatalf("findDetach(%q) = (%d, %d), want (0, %d)", tc.data, idx, length, tc.wantLen)
			}
		})
	}
}

func TestMakeRawDeliversCtrlQFromPTY(t *testing.T) {
	master, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer tty.Close()

	restore, err := makeRaw(tty)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if _, err := master.Write([]byte{defaultExitKey}); err != nil {
		t.Fatal(err)
	}
	type readResult struct {
		value byte
		err   error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		var got [1]byte
		_, err := tty.Read(got[:])
		resultCh <- readResult{value: got[0], err: err}
	}()
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.value != defaultExitKey {
			t.Fatalf("raw PTY delivered %#x, want ctrl+q (%#x)", result.value, defaultExitKey)
		}
	case <-time.After(time.Second):
		t.Fatal("ctrl+q was not delivered by the raw PTY")
	}
}

func TestParseExitKey(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want byte
	}{
		{"ctrl+q", defaultExitKey},
		{"q", defaultExitKey},
		{`ctrl+\`, 0x1c},
		{`\`, 0x1c},
		{" ctrl+a ", 0x01},
		{"CTRL+Z", 0x1a},
		{"ctrl+]", 0x1d},
		{"ctrl+^", 0x1e},
		{"ctrl+_", 0x1f},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := ParseExitKey(tc.spec)
			if err != nil {
				t.Fatalf("ParseExitKey(%q): %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("ParseExitKey(%q) = %#x, want %#x", tc.spec, got, tc.want)
			}
		})
	}
}

func TestParseExitKeyRejectsInvalidAndUnsafeSpecs(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want string
	}{
		{"", "invalid"},
		{"ctrl+&", "invalid"},
		{"f12", "invalid"},
		{"super+f12", "invalid"},
		{"ctrl+space", "invalid"},
		{"ctrl+[", "invalid"},
		{"ctrl+m", "Enter"},
		{"ctrl+j", "Enter"},
		{"ctrl+i", "Tab"},
		{"ctrl+h", "Backspace"},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := ParseExitKey(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseExitKey(%q) = %#x, %v; want error containing %q", tc.spec, got, err, tc.want)
			}
		})
	}
}
