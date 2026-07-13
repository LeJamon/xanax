package wire

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type chunkWriter struct {
	bytes.Buffer
	max int
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.Buffer.Write(p)
}

func TestWriteHandlesShortWrites(t *testing.T) {
	w := &chunkWriter{max: 2}
	payload := []byte("terminal output")
	if err := Write(w, TypeOutput, payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f, err := Read(&w.Buffer)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.Type != TypeOutput || !bytes.Equal(f.Payload, payload) {
		t.Fatalf("round trip = (%v, %q), want (%v, %q)", f.Type, f.Payload, TypeOutput, payload)
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestWriteRejectsZeroLengthWrite(t *testing.T) {
	if err := Write(zeroWriter{}, TypeDetach, nil); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write error = %v, want io.ErrShortWrite", err)
	}
}
