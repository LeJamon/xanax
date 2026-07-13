package ringbuf_test

import (
	"bytes"
	"testing"

	"github.com/LeJamon/rvr/internal/ringbuf"
)

func TestUnderCapacity(t *testing.T) {
	r := ringbuf.New(16)
	r.Write([]byte("hello"))
	r.Write([]byte(" world"))
	if got := r.Snapshot(); !bytes.Equal(got, []byte("hello world")) {
		t.Errorf("Snapshot = %q", got)
	}
	if r.Len() != 11 {
		t.Errorf("Len = %d, want 11", r.Len())
	}
}

func TestOverwriteOldest(t *testing.T) {
	r := ringbuf.New(8)
	r.Write([]byte("abcdefgh")) // exactly fills
	r.Write([]byte("XYЗ"))      // multibyte to exercise wrap; keep last 8 bytes
	got := r.Snapshot()
	if len(got) != 8 {
		t.Fatalf("len = %d, want 8", len(got))
	}
	// The tail must be the most recent 8 bytes of the concatenation.
	full := []byte("abcdefghXYЗ")
	want := full[len(full)-8:]
	if !bytes.Equal(got, want) {
		t.Errorf("Snapshot = %q, want %q", got, want)
	}
}

func TestSingleWriteLargerThanCap(t *testing.T) {
	r := ringbuf.New(4)
	r.Write([]byte("abcdefgh"))
	if got := r.Snapshot(); !bytes.Equal(got, []byte("efgh")) {
		t.Errorf("Snapshot = %q, want efgh", got)
	}
}

func TestWrapAcrossBoundary(t *testing.T) {
	r := ringbuf.New(5)
	r.Write([]byte("abc"))
	r.Write([]byte("de"))  // fills: abcde
	r.Write([]byte("fgh")) // pushes out abc -> defgh
	if got := r.Snapshot(); !bytes.Equal(got, []byte("defgh")) {
		t.Errorf("Snapshot = %q, want defgh", got)
	}
}

func TestManySmallWrites(t *testing.T) {
	r := ringbuf.New(3)
	for _, b := range []byte("abcdefghij") {
		r.Write([]byte{b})
	}
	if got := r.Snapshot(); !bytes.Equal(got, []byte("hij")) {
		t.Errorf("Snapshot = %q, want hij", got)
	}
}
