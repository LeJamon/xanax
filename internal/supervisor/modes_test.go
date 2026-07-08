package supervisor

import (
	"bytes"
	"testing"

	"xanax/internal/ringbuf"
	"xanax/internal/wire"
)

func TestObserveModesTracksSticky(t *testing.T) {
	m := map[int]bool{}
	observeModes(m, []byte("\x1b[?1000h\x1b[?1006h\x1b[?2004h"))
	if !m[1000] || !m[1006] || !m[2004] {
		t.Fatalf("mouse/paste modes not tracked: %v", m)
	}
	// Disabling one clears just it.
	observeModes(m, []byte("\x1b[?1000l"))
	if m[1000] || !m[1006] {
		t.Errorf("disable did not clear 1000 only: %v", m)
	}
	// Non-sticky modes (25 = cursor visibility, 1049 = alt screen) are ignored.
	observeModes(m, []byte("\x1b[?25h\x1b[?1049h"))
	if m[25] || m[1049] {
		t.Errorf("non-sticky mode tracked: %v", m)
	}
}

func TestObserveModesMultiParam(t *testing.T) {
	m := map[int]bool{}
	observeModes(m, []byte("\x1b[?1002;1006h"))
	if !m[1002] || !m[1006] {
		t.Errorf("multi-param set not parsed: %v", m)
	}
}

func TestModeSequenceReenables(t *testing.T) {
	m := map[int]bool{2004: true, 1000: true}
	seq := modeSequence(m)
	if !bytes.Contains(seq, []byte("\x1b[?1000h")) || !bytes.Contains(seq, []byte("\x1b[?2004h")) {
		t.Errorf("mode sequence = %q", seq)
	}
	if modeSequence(map[int]bool{}) != nil {
		t.Error("empty mode set should produce no sequence")
	}
}

func TestModeSequenceStableOrder(t *testing.T) {
	// Numeric order is deterministic regardless of map iteration.
	got := modeSequence(map[int]bool{2004: true, 1000: true, 1006: true})
	want := []byte("\x1b[?1000h\x1b[?1006h\x1b[?2004h")
	if !bytes.Equal(got, want) {
		t.Errorf("mode sequence order = %q, want %q", got, want)
	}
}

func TestObserveKittyKeyboardTracksPushPop(t *testing.T) {
	var keys kittyKeyboardState

	observeKittyKeyboard(&keys, []byte("\x1b[>1u\x1b[>5u"))
	if got, want := keys.sequence(), []byte("\x1b[>1u\x1b[>5u"); !bytes.Equal(got, want) {
		t.Fatalf("kitty sequence = %q, want %q", got, want)
	}

	observeKittyKeyboard(&keys, []byte("\x1b[<u"))
	if got, want := keys.sequence(), []byte("\x1b[>1u"); !bytes.Equal(got, want) {
		t.Fatalf("kitty sequence after one pop = %q, want %q", got, want)
	}

	observeKittyKeyboard(&keys, []byte("\x1b[<u"))
	if got := keys.sequence(); got != nil {
		t.Fatalf("kitty sequence after final pop = %q, want nil", got)
	}
}

func TestObserveKittyKeyboardTracksSetAndPopCount(t *testing.T) {
	var keys kittyKeyboardState

	observeKittyKeyboard(&keys, []byte("\x1b[=2u\x1b[>5u\x1b[>7u"))
	observeKittyKeyboard(&keys, []byte("\x1b[<u"))
	if got, want := keys.sequence(), []byte("\x1b[=2u\x1b[>5u"); !bytes.Equal(got, want) {
		t.Fatalf("kitty sequence after set/push/pop = %q, want %q", got, want)
	}

	observeKittyKeyboard(&keys, []byte("\x1b[<2u"))
	if got := keys.sequence(); got != nil {
		t.Fatalf("kitty sequence after counted pop = %q, want nil", got)
	}
}

func TestObserveKittyKeyboardSetCanAddAndRemoveFlags(t *testing.T) {
	var keys kittyKeyboardState

	observeKittyKeyboard(&keys, []byte("\x1b[=1u\x1b[=2;2u\x1b[=1;3u"))
	if got, want := keys.sequence(), []byte("\x1b[=2u"); !bytes.Equal(got, want) {
		t.Fatalf("kitty add/remove sequence = %q, want %q", got, want)
	}
}

func TestStripKittyKeyboardSequences(t *testing.T) {
	in := []byte("a\x1b[>1ub\x1b[=2uc\x1b[<ud")
	if got, want := stripKittyKeyboardSequences(in), []byte("abcd"); !bytes.Equal(got, want) {
		t.Fatalf("stripKittyKeyboardSequences = %q, want %q", got, want)
	}

	plain := []byte("plain")
	if got := stripKittyKeyboardSequences(plain); !bytes.Equal(got, plain) {
		t.Fatalf("plain bytes changed: %q", got)
	}
}

func TestRegisterReplaysKittyKeyboardStateOnce(t *testing.T) {
	h := newHub(ringbuf.New(1024), newScreen(20, 5), wire.Info{SessionID: "kitty"})
	h.broadcastOutput([]byte("before\x1b[>1uafter"))

	cl := &client{out: make(chan wire.Frame, 8), done: make(chan struct{})}
	h.register(cl, false)

	var got []byte
	for len(cl.out) > 0 {
		f := <-cl.out
		if f.Type == wire.TypeOutput {
			got = append(got, f.Payload...)
		}
	}
	if !bytes.Contains(got, []byte("beforeafter")) {
		t.Fatalf("scrollback content missing after stripping kitty sequence: %q", got)
	}
	if bytes.Contains(got, []byte("before\x1b[>1uafter")) {
		t.Fatalf("raw stackful kitty sequence remained in scrollback: %q", got)
	}
	if !bytes.Contains(got, kittyKeyboardResetSequence()) {
		t.Fatalf("kitty reset missing from attach primer: %q", got)
	}
	if count := bytes.Count(got, []byte("\x1b[>1u")); count != 1 {
		t.Fatalf("kitty replay count = %d, want 1; payload %q", count, got)
	}
}
