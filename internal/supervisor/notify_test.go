package supervisor

import (
	"io"
	"log/slog"
	"testing"

	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/wire"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNotificationFor(t *testing.T) {
	cases := []struct {
		name       string
		prev, next session.Status
		detail     string
		wantFire   bool
		wantInBody string
	}{
		{"running->idle", session.StatusRunning, session.StatusIdle, "", true, "ready"},
		{"running->waiting", session.StatusRunning, session.StatusWaiting, "which API?", true, "which API?"},
		{"waiting->waiting no repeat", session.StatusWaiting, session.StatusWaiting, "x", false, ""},
		{"running->completed", session.StatusRunning, session.StatusCompleted, "", true, "finished"},
		{"running->failed", session.StatusRunning, session.StatusFailed, "boom", true, "boom"},
		{"running->cancelled is quiet", session.StatusRunning, session.StatusCancelled, "", false, ""},
		{"running->running quiet", session.StatusRunning, session.StatusRunning, "", false, ""},
		{"waiting->running quiet", session.StatusWaiting, session.StatusRunning, "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := notificationFor(c.prev, c.next, "my task", c.detail)
			if ok != c.wantFire {
				t.Fatalf("fire = %v, want %v", ok, c.wantFire)
			}
			if ok {
				if ev.title == "" || ev.body == "" {
					t.Errorf("empty title/body: %+v", ev)
				}
				if c.wantInBody != "" && !contains(ev.body, c.wantInBody) {
					t.Errorf("body %q missing %q", ev.body, c.wantInBody)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRaiseNotificationSuppression(t *testing.T) {
	var calls int
	s := &Supervisor{
		opts:     Options{Session: &session.Session{Title: "t"}, Notify: true},
		notifyFn: func(string, string) { calls++ },
		log:      silentLogger(),
	}

	// Enabled, not attached → fires.
	s.raiseNotification(session.StatusRunning, session.StatusWaiting, "d")
	if calls != 1 {
		t.Fatalf("expected 1 notification, got %d", calls)
	}

	// Disabled → silent.
	s.opts.Notify = false
	s.raiseNotification(session.StatusRunning, session.StatusFailed, "d")
	if calls != 1 {
		t.Errorf("disabled notify still fired (%d)", calls)
	}

	// Enabled but a client is attached → suppressed.
	s.opts.Notify = true
	s.hub = newHub(nil, nil, wire.Info{})
	s.hub.clients[&client{}] = struct{}{}
	s.raiseNotification(session.StatusRunning, session.StatusCompleted, "")
	if calls != 1 {
		t.Errorf("notification fired while attached (%d)", calls)
	}
}
