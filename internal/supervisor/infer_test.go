package supervisor

import (
	"testing"

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
	// The inferer starts in "running" (the supervisor already set it), so
	// non-matching output is not a transition.
	if _, _, ok := in.observe([]byte("building the thing...")); ok {
		t.Error("running->running should not report a change")
	}
	// Matching output → waiting.
	st, _, ok := in.observe([]byte("Proceed? (y/n)"))
	if !ok || st != session.StatusWaiting {
		t.Fatalf("matching output: st=%v ok=%v, want waiting", st, ok)
	}
	// Output resumes (non-matching) → running.
	st, _, ok = in.observe([]byte("ok, doing it"))
	if !ok || st != session.StatusRunning {
		t.Fatalf("resume: st=%v ok=%v, want running", st, ok)
	}
}

func TestInfererIdleTimeout(t *testing.T) {
	in := newInferer(config.Harness{IdleTimeout: 1}, silentLogger())
	if in == nil {
		t.Fatal("inferer should be enabled")
	}
	// Fresh output → not yet idle.
	in.observe([]byte("x"))
	if _, _, ok := in.idleCheck(); ok {
		t.Error("should not be idle immediately after output")
	}
	// Simulate 2s of silence by backdating lastOutput.
	in.lastOutput.Store(nowNano() - int64(2)*1e9)
	st, detail, ok := in.idleCheck()
	if !ok || st != session.StatusWaiting || detail != "idle" {
		t.Fatalf("idleCheck after timeout: st=%v detail=%q ok=%v", st, detail, ok)
	}
	// Already waiting → no repeat.
	if _, _, ok := in.idleCheck(); ok {
		t.Error("idleCheck should not fire twice while already waiting")
	}
	// Output resumes → running.
	st, _, ok = in.observe([]byte("back to work"))
	if !ok || st != session.StatusRunning {
		t.Fatalf("resume after idle: st=%v ok=%v", st, ok)
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
