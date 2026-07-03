package adapter

import (
	"testing"

	"xanax/internal/config"
	"xanax/internal/session"
)

// The supervisor calls adapter.Close() twice on the happy path — once
// explicitly (to stop the state channel before waiting on its goroutines) and
// once via a deferred catch-all for early-return error paths. A native adapter
// closes its state channel in Close, so a second Close must not panic with
// "close of closed channel".
func TestOpencodeCloseIdempotent(t *testing.T) {
	sess := &session.Session{ID: "oc-close", RepoPath: t.TempDir()}
	a, err := newOpencode(sess, config.Harness{Adapter: config.AdapterOpencode, Command: "opencode"}, testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, ok := <-a.States(); ok {
		t.Error("states channel should be closed after Close")
	}
}

func TestPiCloseIdempotent(t *testing.T) {
	sess := &session.Session{ID: "pi-close", RepoPath: t.TempDir()}
	a, err := newPi(sess, config.Harness{Adapter: config.AdapterPi, Command: "pi"}, testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, ok := <-a.States(); ok {
		t.Error("states channel should be closed after Close")
	}
}
