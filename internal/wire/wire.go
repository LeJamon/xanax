// Package wire is the framed protocol spoken over a session's unix socket
// between the supervisor and attached clients (SPEC.md §4).
//
// Every frame is [type:1][length:uint32 BE][payload:length]. Control frames
// carry JSON payloads; output/input frames carry raw PTY bytes. A single
// framing for both keeps readers simple and self-synchronizing.
package wire

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

type Type byte

const (
	// Supervisor → client.
	TypeHello  Type = 1 // JSON Info, sent once on connect
	TypeOutput Type = 2 // raw PTY bytes (also used for snapshot replay)
	TypeState  Type = 3 // JSON State
	TypeExit   Type = 4 // JSON Exit, sent once when the session ends

	// Client → supervisor.
	TypeInput  Type = 5 // raw bytes to the PTY
	TypeResize Type = 6 // JSON Resize
	TypeKill   Type = 7 // request termination (empty payload)
	TypeDetach Type = 8 // client leaving (empty payload; closing also works)

	// Peek: a one-shot preview without attaching.
	TypeSnapshotReq Type = 9  // client→supervisor, JSON Resize (max rows/cols)
	TypeSnapshot    Type = 10 // supervisor→client, plain-text preview bytes
)

// MaxFrame caps a single payload to guard against corruption/DoS.
const MaxFrame = 1 << 20 // 1 MiB

// Info is the TypeHello payload: a snapshot of the session at connect time.
type Info struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Harness   string `json:"harness"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
}

// State is the TypeState payload: a lifecycle transition.
type State struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Exit is the TypeExit payload: the terminal outcome.
type Exit struct {
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
}

// Resize is the TypeResize payload: the client's terminal dimensions.
type Resize struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// Frame is a decoded protocol frame.
type Frame struct {
	Type    Type
	Payload []byte
}

// Write encodes a single frame. Callers holding a shared writer must
// serialize Write calls themselves (frames must not interleave).
func Write(w io.Writer, t Type, payload []byte) error {
	if len(payload) > MaxFrame {
		return fmt.Errorf("wire: frame payload too large (%d bytes)", len(payload))
	}
	var hdr [5]byte
	hdr[0] = byte(t)
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if err := writeFull(w, hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if err := writeFull(w, payload); err != nil {
			return err
		}
	}
	return nil
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// WriteJSON marshals v and writes it as a frame of type t.
func WriteJSON(w io.Writer, t Type, v any) error {
	f, err := MarshalFrame(t, v)
	if err != nil {
		return err
	}
	return Write(w, f.Type, f.Payload)
}

// MarshalFrame builds a frame with a JSON-encoded payload.
func MarshalFrame(t Type, v any) (Frame, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Frame{}, fmt.Errorf("wire: marshal %T: %w", v, err)
	}
	return Frame{Type: t, Payload: b}, nil
}

// Read decodes the next frame from r.
func Read(r io.Reader) (Frame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > MaxFrame {
		return Frame{}, fmt.Errorf("wire: incoming frame too large (%d bytes)", n)
	}
	f := Frame{Type: Type(hdr[0])}
	if n > 0 {
		f.Payload = make([]byte, n)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, err
		}
	}
	return f, nil
}

// DecodeJSON unmarshals the frame payload into v.
func (f Frame) DecodeJSON(v any) error {
	return json.Unmarshal(f.Payload, v)
}
