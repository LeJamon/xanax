package supervisor

import (
	"testing"

	"github.com/LeJamon/rvr/internal/adapter"
	"github.com/LeJamon/rvr/internal/session"
)

func TestMapState(t *testing.T) {
	cases := []struct {
		kind    adapter.StateKind
		want    session.Status
		change  bool
		message string
	}{
		{adapter.StateBusy, session.StatusRunning, true, ""},
		{adapter.StateIdle, session.StatusIdle, true, ""},
		{adapter.StateNeedsInput, session.StatusWaiting, true, "which API?"},
		{adapter.StateError, "", false, "boom"},
	}
	for _, c := range cases {
		st, detail, change := mapState(adapter.StateEvent{Kind: c.kind, Message: c.message})
		if change != c.change {
			t.Errorf("kind %v: change = %v, want %v", c.kind, change, c.change)
		}
		if change && st != c.want {
			t.Errorf("kind %v: status = %q, want %q", c.kind, st, c.want)
		}
		if c.kind == adapter.StateNeedsInput && detail != c.message {
			t.Errorf("needs_input detail = %q, want %q", detail, c.message)
		}
	}
}

func TestClassifyExit(t *testing.T) {
	cases := []struct {
		name string
		in   exitInfo
		want session.Status
	}{
		{"killed", exitInfo{killedByUs: true, exitCode: 137}, session.StatusCancelled},
		{"nonzero exit", exitInfo{exitCode: 1}, session.StatusFailed},
		{"clean exit no channel", exitInfo{exitCode: 0}, session.StatusCompleted},
		{
			"clean exit while idle",
			exitInfo{exitCode: 0, hadStateChannel: true, sawState: true, lastKind: adapter.StateIdle},
			session.StatusCompleted,
		},
		{
			"clean exit while busy is a failure",
			exitInfo{exitCode: 0, hadStateChannel: true, sawState: true, lastKind: adapter.StateBusy},
			session.StatusFailed,
		},
		{
			"clean exit, channel present but no events yet",
			exitInfo{exitCode: 0, hadStateChannel: true, sawState: false},
			session.StatusCompleted,
		},
	}
	for _, c := range cases {
		if got := classifyExit(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
