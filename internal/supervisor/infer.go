package supervisor

import (
	"log/slog"
	"regexp"
	"sync/atomic"
	"time"

	"xanax/internal/config"
	"xanax/internal/session"
)

// inferer approximates agent state for a harness that exposes no native state
// channel (the generic adapter). It has two independent, opt-in signals:
//   - a regexp matched against output → "waiting" (e.g. a "(y/n)" prompt)
//   - an idle timeout: no output for N seconds → "waiting"
//
// Either resets to "running" when output resumes. It is best-effort and
// approximate; a native adapter is authoritative.
type inferer struct {
	idle       time.Duration
	waitingRe  *regexp.Regexp
	lastOutput atomic.Int64 // unix nanoseconds
	waiting    atomic.Bool
}

// newInferer returns a configured inferer, or nil when the harness enabled
// neither signal.
func newInferer(h config.Harness, log *slog.Logger) *inferer {
	in := &inferer{idle: time.Duration(h.IdleTimeout) * time.Second}
	if h.WaitingPattern != "" {
		re, err := regexp.Compile(h.WaitingPattern)
		if err != nil {
			log.Warn("invalid waiting_pattern; ignoring", "err", err)
		} else {
			in.waitingRe = re
		}
	}
	if in.idle == 0 && in.waitingRe == nil {
		return nil
	}
	in.lastOutput.Store(time.Now().UnixNano())
	return in
}

// observe processes an output chunk. It returns a status transition to apply,
// or ok=false when nothing changed. Output means the agent is active, unless it
// matches the waiting pattern (the prompt just appeared).
func (in *inferer) observe(chunk []byte) (session.Status, string, bool) {
	in.lastOutput.Store(nowNano())
	waiting := in.waitingRe != nil && in.waitingRe.Match(chunk)
	if in.waiting.Swap(waiting) == waiting {
		return "", "", false // no change
	}
	if waiting {
		return session.StatusWaiting, "", true
	}
	return session.StatusRunning, "", true
}

// idleCheck marks waiting when output has been quiet past the idle timeout.
// Returns a transition to apply, or ok=false.
func (in *inferer) idleCheck() (session.Status, string, bool) {
	if in.idle == 0 || in.waiting.Load() {
		return "", "", false
	}
	if time.Duration(nowNano()-in.lastOutput.Load()) < in.idle {
		return "", "", false
	}
	if in.waiting.Swap(true) {
		return "", "", false
	}
	return session.StatusWaiting, "idle", true
}

func nowNano() int64 { return time.Now().UnixNano() }

// idleLoop runs the idle check on a ticker until the session exits.
func (s *Supervisor) idleLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.exited:
			return
		case <-t.C:
			if st, detail, ok := s.infer.idleCheck(); ok {
				s.applyStatus(st, detail)
			}
		}
	}
}
