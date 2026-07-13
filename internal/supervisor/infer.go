package supervisor

import (
	"log/slog"
	"regexp"
	"time"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
)

// inferer approximates agent state for a harness that exposes no native state
// channel (the generic adapter). It has two independent, opt-in signals:
//   - a regexp matched against output → "waiting" (e.g. a "(y/n)" prompt)
//   - an idle timeout: no output for N seconds → "idle"
//
// Either resets to "running" when output resumes. It is best-effort and
// approximate; a native adapter is authoritative.
//
// The inferer holds only immutable config plus lastOutput; the current state is
// the supervisor's curStatus. All of its methods are pure or read lastOutput,
// and the supervisor drives them under statusMu so an output signal and an idle
// signal are always applied in the order they occurred (see applyStatusLocked).
type inferer struct {
	idle       time.Duration
	waitingRe  *regexp.Regexp
	lastOutput time.Time // guarded by Supervisor.statusMu
}

// newInferer returns a configured inferer, or nil when the harness enabled
// neither signal.
func newInferer(h config.Harness, log *slog.Logger) *inferer {
	in := &inferer{idle: time.Duration(h.IdleTimeout) * time.Second, lastOutput: time.Now()}
	if h.WaitingPattern != "" {
		re, err := regexp.Compile(h.WaitingPattern)
		if err != nil {
			log.Warn("invalid waiting_pattern; ignoring", "err", err)
		} else {
			in.waitingRe = re
		}
	}
	if in.idle <= 0 && in.waitingRe == nil {
		return nil
	}
	return in
}

// observedStatus returns the status implied by an output chunk: waiting if it
// matches the waiting pattern (a prompt just appeared), else running. Pure — it
// reads only immutable state, so it needs no lock.
func (in *inferer) observedStatus(chunk []byte) session.Status {
	if in.waitingRe != nil && in.waitingRe.Match(chunk) {
		return session.StatusWaiting
	}
	return session.StatusRunning
}

// idleElapsed reports whether the idle timeout has passed since the last output.
// The caller holds statusMu (lastOutput is guarded by it).
func (in *inferer) idleElapsed(now time.Time) bool {
	return in.idle > 0 && now.Sub(in.lastOutput) >= in.idle
}

// inferObserve applies the status implied by an output chunk and records the
// output time, under statusMu so a concurrent idle tick sees a consistent
// (lastOutput, status) pair — fresh output can never be lost behind a stale
// idle decision.
func (s *Supervisor) inferObserve(chunk []byte) {
	status := s.infer.observedStatus(chunk) // pure; compute before locking
	s.statusMu.Lock()
	s.infer.lastOutput = time.Now()
	prev, changed := s.applyStatusLocked(status, "")
	s.statusMu.Unlock()
	if changed {
		s.raiseNotification(prev, status, "")
	}
}

// idleLoop marks the session idle once output has been quiet past the idle
// timeout, on a one-second ticker until the session exits. The decision and its
// application are made together under statusMu, so an output chunk arriving in
// the same instant is never overwritten.
func (s *Supervisor) idleLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.exited:
			return
		case <-t.C:
			s.statusMu.Lock()
			if s.curStatus != session.StatusIdle && s.infer.idleElapsed(time.Now()) {
				s.applyStatusLocked(session.StatusIdle, "")
			}
			s.statusMu.Unlock()
			// Silence alone is not actionable, so generic idle inference never
			// raises a notification. Native StateIdle events still may.
		}
	}
}
