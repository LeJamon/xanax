package supervisor

import (
	"github.com/LeJamon/rvr/internal/adapter"
	"github.com/LeJamon/rvr/internal/session"
)

// mapState maps a normalized adapter state event to a session status and
// detail. change is false when the event should be recorded but not alter the
// status (e.g. a non-fatal harness error).
func mapState(ev adapter.StateEvent) (status session.Status, detail string, change bool) {
	switch ev.Kind {
	case adapter.StateBusy:
		return session.StatusRunning, "", true
	case adapter.StateIdle:
		return session.StatusIdle, ev.Message, true
	case adapter.StateNeedsInput:
		return session.StatusWaiting, ev.Message, true
	case adapter.StateError:
		return "", ev.Message, false
	default:
		return "", "", false
	}
}

// exitInfo captures everything needed to classify a session's terminal state.
type exitInfo struct {
	killedByUs      bool
	exitCode        int
	hadStateChannel bool
	sawState        bool
	lastKind        adapter.StateKind
}

// classifyExit decides the terminal status at process exit (SPEC.md §6).
// Exit codes alone are unreliable for these harnesses, so a clean exit while
// the agent was still busy is treated as a failure.
func classifyExit(info exitInfo) session.Status {
	switch {
	case info.killedByUs:
		return session.StatusCancelled
	case info.exitCode != 0:
		return session.StatusFailed
	case info.hadStateChannel && info.sawState && info.lastKind == adapter.StateBusy:
		return session.StatusFailed
	default:
		return session.StatusCompleted
	}
}
