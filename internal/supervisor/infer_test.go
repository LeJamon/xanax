package supervisor

import (
	"testing"
	"time"

	"xanax/internal/config"
	"xanax/internal/session"
)

func TestNewInfererDisabledWhenUnconfigured(t *testing.T) {
	if newInferer(config.Harness{}, silentLogger()) != nil {
		t.Error("inferer should be nil with no idle_timeout or waiting_pattern")
	}
}

func TestInfererWaitingPattern(t *testing.T) {
	in := newInferer(config.Harness{WaitingPattern: `\(y/n\)`}, silentLogger())
	if in == nil {
		t.Fatal("inferer should be enabled")
	}
	if st := in.observedStatus([]byte("building the thing...")); st != session.StatusRunning {
		t.Errorf("non-matching output: st=%v, want running", st)
	}
	if st := in.observedStatus([]byte("Proceed? (y/n)")); st != session.StatusWaiting {
		t.Errorf("matching output: st=%v, want waiting", st)
	}
	// Output that no longer matches → running again (the supervisor applies the
	// transition; here we assert the implied status).
	if st := in.observedStatus([]byte("ok, doing it")); st != session.StatusRunning {
		t.Errorf("resume: st=%v, want running", st)
	}
}

func TestInfererNoPatternAlwaysRunning(t *testing.T) {
	// With only idle configured there is no pattern, so every chunk is "running".
	in := newInferer(config.Harness{IdleTimeout: 5}, silentLogger())
	if in == nil {
		t.Fatal("inferer should be enabled")
	}
	if st := in.observedStatus([]byte("Proceed? (y/n)")); st != session.StatusRunning {
		t.Errorf("no pattern: st=%v, want running (matching is disabled)", st)
	}
}

func TestInfererIdleElapsed(t *testing.T) {
	in := newInferer(config.Harness{IdleTimeout: 1}, silentLogger())
	if in == nil {
		t.Fatal("inferer should be enabled")
	}
	now := time.Now()
	in.lastOutput = now
	if in.idleElapsed(now) {
		t.Error("should not be idle immediately after output")
	}
	// Two seconds of silence exceeds the one-second timeout.
	if !in.idleElapsed(now.Add(2 * time.Second)) {
		t.Error("idleElapsed should fire once the timeout has passed")
	}
}

func TestInfererIdleDisabledWhenZero(t *testing.T) {
	// A pattern-only inferer has idle == 0; idleElapsed must never fire, else the
	// idle checker would flip the session waiting on every tick.
	in := newInferer(config.Harness{WaitingPattern: `\(y/n\)`}, silentLogger())
	if in == nil {
		t.Fatal("inferer should be enabled")
	}
	if in.idleElapsed(in.lastOutput.Add(time.Hour)) {
		t.Error("idleElapsed must not fire when idle_timeout is unset")
	}
}

func TestInfererInvalidPatternIgnored(t *testing.T) {
	// Bad regex + no idle → nil (nothing enabled).
	if newInferer(config.Harness{WaitingPattern: "("}, silentLogger()) != nil {
		t.Error("invalid pattern with no other signal should yield nil inferer")
	}
	// Bad regex but idle set → enabled via idle only.
	in := newInferer(config.Harness{WaitingPattern: "(", IdleTimeout: 5}, silentLogger())
	if in == nil || in.waitingRe != nil {
		t.Error("invalid pattern should be dropped but idle kept")
	}
}
