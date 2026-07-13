// Package sessionctl coordinates lifecycle operations that must agree with a
// session's supervisor process.
package sessionctl

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/supervisor"
)

const (
	defaultStopTimeout  = 10 * time.Second
	defaultPollInterval = 50 * time.Millisecond
	killRetryInterval   = 250 * time.Millisecond
)

// ErrActive means removal was attempted without force while a session status,
// socket, or supervisor lease still indicated activity.
var ErrActive = errors.New("session is active")

// ActiveError describes the session that requires forced removal.
type ActiveError struct {
	Session *session.Session
	Reason  string
}

func (e *ActiveError) Error() string { return fmt.Sprintf("session %s %s", e.Session.ID, e.Reason) }
func (e *ActiveError) Unwrap() error { return ErrActive }

// Options controls a removal operation. Alive and Kill default to the attach
// package implementations; the overrides make process coordination
// deterministic in tests.
type Options struct {
	SocketDir    string
	Force        bool
	SkipActive   bool
	StopTimeout  time.Duration
	PollInterval time.Duration
	Alive        func(string) bool
	Kill         func(string) error
}

// Result describes one removed session.
type Result struct {
	Session *session.Session
	Killed  bool
}

type target struct {
	sess       *session.Session
	socketPath string
	lease      *supervisor.Lease
	owned      bool
	alive      bool
	killed     bool
}

// Remove resolves ids (full IDs or unique prefixes), synchronizes each target
// with its supervisor lease, and deletes the rows. A lease remains held through
// deletion, so a concurrently spawned supervisor cannot launch from a stale
// row. SkipActive is used by prune to leave concurrently resumed sessions
// untouched and releases each target before moving to the next.
func Remove(st *store.Store, ids []string, opts Options) ([]Result, error) {
	opts.withDefaults()
	if !opts.Force {
		return removeTerminalSessions(st, ids, opts)
	}
	targets, err := resolve(st, ids, opts)
	if err != nil {
		return nil, err
	}
	defer releaseTargets(targets)

	for _, target := range targets {
		if err := stopAndOwn(target, opts); err != nil {
			return nil, fmt.Errorf("stop session %s before remove: %w", target.sess.ID, err)
		}
	}

	removed := make([]Result, 0, len(targets))
	for _, target := range targets {
		if err := st.DeleteSession(target.sess.ID); err != nil {
			return nil, fmt.Errorf("remove session %s: %w", target.sess.ID, err)
		}
		removed = append(removed, Result{Session: target.sess, Killed: target.killed})
	}
	return removed, nil
}

// removeTerminalSessions is prune's per-target path. It resolves every ID
// before mutating the store, then holds at most one lease at a time. A resumed
// supervisor therefore waits only for its own conditional delete, not for an
// arbitrarily large prune batch.
func removeTerminalSessions(st *store.Store, ids []string, opts Options) ([]Result, error) {
	sessions, err := resolveSessions(st, ids)
	if err != nil {
		return nil, err
	}
	if !opts.SkipActive {
		for _, sess := range sessions {
			if !sess.Status.Terminal() {
				return nil, &ActiveError{Session: sess, Reason: activeReason(sess, false, false)}
			}
		}
	}
	removed := make([]Result, 0, len(sessions))
	for _, sess := range sessions {
		if !sess.Status.Terminal() {
			continue
		}
		socketPath := filepath.Join(opts.SocketDir, sess.ID+".sock")
		lease, err := supervisor.TryAcquireLease(socketPath)
		if errors.Is(err, supervisor.ErrAlreadySupervised) {
			if opts.SkipActive {
				continue
			}
			return nil, &ActiveError{Session: sess, Reason: activeReason(sess, false, true)}
		}
		if err != nil {
			return nil, fmt.Errorf("acquire supervisor lease for %s: %w", sess.ID, err)
		}
		if opts.Alive(socketPath) {
			lease.Release()
			if opts.SkipActive {
				continue
			}
			return nil, &ActiveError{Session: sess, Reason: activeReason(sess, true, false)}
		}
		deleted, deleteErr := st.DeleteTerminalSession(sess.ID)
		lease.Release()
		if deleteErr != nil {
			return nil, fmt.Errorf("remove terminal session %s: %w", sess.ID, deleteErr)
		}
		if deleted {
			removed = append(removed, Result{Session: sess})
			continue
		}
		if opts.SkipActive {
			continue
		}
		current, err := st.GetSession(sess.ID)
		if err != nil {
			return nil, err
		}
		return nil, &ActiveError{Session: current, Reason: activeReason(current, false, false)}
	}
	return removed, nil
}

func (opts *Options) withDefaults() {
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = defaultStopTimeout
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	if opts.Alive == nil {
		opts.Alive = attach.Alive
	}
	if opts.Kill == nil {
		opts.Kill = attach.Kill
	}
}

func resolve(st *store.Store, ids []string, opts Options) ([]*target, error) {
	sessions, err := resolveSessions(st, ids)
	if err != nil {
		return nil, err
	}
	targets := make([]*target, 0, len(sessions))
	for _, sess := range sessions {
		socketPath := filepath.Join(opts.SocketDir, sess.ID+".sock")
		lease, err := supervisor.TryAcquireLease(socketPath)
		owned := errors.Is(err, supervisor.ErrAlreadySupervised)
		if err != nil && !owned {
			releaseTargets(targets)
			return nil, fmt.Errorf("acquire supervisor lease for %s: %w", sess.ID, err)
		}
		targets = append(targets, &target{
			sess:       sess,
			socketPath: socketPath,
			lease:      lease,
			owned:      owned,
			alive:      opts.Alive(socketPath),
		})
	}
	return targets, nil
}

func resolveSessions(st *store.Store, ids []string) ([]*session.Session, error) {
	seen := make(map[string]bool, len(ids))
	sessions := make([]*session.Session, 0, len(ids))
	for _, id := range ids {
		sess, err := st.GetSession(id)
		if err != nil {
			return nil, err
		}
		if seen[sess.ID] {
			continue
		}
		seen[sess.ID] = true
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func stopAndOwn(target *target, opts Options) error {
	if !target.owned {
		if target.alive {
			if err := opts.Kill(target.socketPath); err != nil {
				return err
			}
			target.killed = true
		}
		return nil
	}

	deadline := time.Now().Add(opts.StopTimeout)
	var lastKill time.Time
	for {
		lease, err := supervisor.TryAcquireLease(target.socketPath)
		switch {
		case err == nil:
			target.lease = lease
			target.owned = false
			return nil
		case !errors.Is(err, supervisor.ErrAlreadySupervised):
			return err
		}

		now := time.Now()
		if opts.Alive(target.socketPath) &&
			(lastKill.IsZero() || now.Sub(lastKill) >= killRetryInterval) {
			if err := opts.Kill(target.socketPath); err != nil {
				return err
			}
			lastKill = now
			target.killed = true
		}
		if !now.Before(deadline) {
			return fmt.Errorf("supervisor did not stop within %s", opts.StopTimeout)
		}
		time.Sleep(opts.PollInterval)
	}
}

func releaseTargets(targets []*target) {
	for _, target := range targets {
		target.lease.Release()
		target.lease = nil
	}
}

func activeReason(sess *session.Session, alive, owned bool) string {
	if sess.Status.Live() {
		return "is " + string(sess.Status)
	}
	if alive {
		return "is still reachable"
	}
	if owned {
		return "has an active supervisor"
	}
	return "changed before it could be removed"
}
