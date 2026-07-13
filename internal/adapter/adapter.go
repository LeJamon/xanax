// Package adapter translates rvr session commands into harness-specific
// behavior. Adapters own how a harness is launched, how the initial prompt is
// delivered, and how running/waiting state is observed (SPEC.md §5). They do
// no prompting, planning, or memory work themselves.
package adapter

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
)

// StateKind is a normalized agent state, harness-agnostic.
type StateKind int

const (
	StateBusy       StateKind = iota // agent actively working
	StateIdle                        // agent idle or ready after finishing a turn
	StateNeedsInput                  // agent explicitly asked the user something
	StateError                       // harness reported a non-fatal error
)

func (k StateKind) String() string {
	switch k {
	case StateBusy:
		return "busy"
	case StateIdle:
		return "idle"
	case StateNeedsInput:
		return "needs_input"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// StateEvent is one normalized state transition emitted by an adapter.
type StateEvent struct {
	Kind    StateKind
	Message string // optional detail (e.g. the permission question)
	At      time.Time
}

// LaunchSpec is everything the supervisor needs to exec a harness in a PTY.
type LaunchSpec struct {
	Path string   // executable (looked up on PATH if not absolute)
	Args []string // arguments after the executable name
	Env  []string // full environment; nil means inherit the supervisor's
	Dir  string   // working directory
}

// Adapter is the per-session harness driver. One instance backs one session
// for the lifetime of its supervisor.
type Adapter interface {
	// Launch returns the command to exec. resume selects native session
	// resumption using the session's HarnessSessionRef when supported.
	Launch(resume bool) (LaunchSpec, error)

	// AfterStart runs once the PTY is live. It delivers the initial prompt
	// (via pty for line-based harnesses, or via an API/side channel) and
	// starts any state-watching machinery. pty writes reach the harness stdin.
	AfterStart(pty io.Writer) error

	// States returns the stream of normalized state events, or nil if this
	// harness exposes no state channel (the session then only reports
	// running/terminal states). The channel is closed by Close.
	States() <-chan StateEvent

	// SessionRef returns the harness-native resume handle discovered so far,
	// or "" if none is known yet. Read repeatedly; it may populate after start.
	SessionRef() string

	// Close releases resources (sockets, HTTP clients, watcher goroutines).
	Close() error
}

// Deps are the shared services handed to every adapter factory.
type Deps struct {
	Paths  config.Paths
	Logger *slog.Logger
}

// Factory builds an adapter for a session given its resolved harness config.
type Factory func(sess *session.Session, h config.Harness, deps Deps) (Adapter, error)

var registry = map[string]Factory{
	config.AdapterGeneric:  newGeneric,
	config.AdapterPi:       newPi,
	config.AdapterOpencode: newOpencode,
}

// New constructs the adapter named by h.Adapter for sess.
func New(sess *session.Session, h config.Harness, deps Deps) (Adapter, error) {
	f, ok := registry[h.Adapter]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", h.Adapter)
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return f(sess, h, deps)
}
