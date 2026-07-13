package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

// socketPath is the unix socket for a session's supervisor.
func (e *env) socketPath(id string) string {
	return filepath.Join(e.paths.SocketDir, id+".sock")
}

// canResume reports whether a dead session can be relaunched. It delegates to
// config.Config.CanResume so the CLI and the dashboard share one definition.
func (e *env) canResume(sess *session.Session) bool {
	return e.cfg.CanResume(sess)
}

func (e *env) checkHarnessCommand(name string, h config.Harness) error {
	if h.Command == "" {
		return fmt.Errorf("harness %q has no command configured; set [harness.%s].command in %s",
			name, name, e.paths.ConfigFile)
	}
	if _, err := exec.LookPath(h.Command); err != nil {
		return fmt.Errorf("harness %q command %q is not available: %w\nInstall the harness, or set [harness.%s].command in %s",
			name, h.Command, err, name, e.paths.ConfigFile)
	}
	return nil
}

func (e *env) supervisorLogPath(id string) string {
	return filepath.Join(e.paths.LogsDir, id+".supervisor.log")
}

func (e *env) waitForSocketOrTerminal(
	st *store.Store,
	id string,
	timeout time.Duration,
) (alive bool, terminal *session.Session, err error) {
	deadline := time.Now().Add(timeout)
	path := e.socketPath(id)
	for time.Now().Before(deadline) {
		if attach.Alive(path) {
			return true, nil, nil
		}
		sess, err := st.GetSession(id)
		if err != nil {
			return false, nil, err
		}
		if sess.Status.Terminal() {
			return false, sess, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	sess, err := st.GetSession(id)
	if err != nil {
		return false, nil, err
	}
	if sess.Status.Terminal() {
		return false, sess, nil
	}
	return false, nil, nil
}

func (e *env) supervisorStartingError(id string, timeout time.Duration) error {
	return fmt.Errorf("supervisor for session %s did not become reachable within %s\nSupervisor log: %s",
		shortID(id), timeout, e.supervisorLogPath(id))
}

func (e *env) sessionUnavailableError(st *store.Store, sess *session.Session) error {
	if sess.Status.Terminal() {
		if sess.Status == session.StatusFailed {
			return fmt.Errorf("%s\nInspect raw output with: rvr logs %s",
				e.failureSummary(st, sess), shortID(sess.ID))
		}
		if e.canResume(sess) {
			return fmt.Errorf("session %s has ended (%s); use `rvr resume %s`",
				shortID(sess.ID), sess.Status, shortID(sess.ID))
		}
		return fmt.Errorf("session %s has ended (%s); inspect logs with `rvr logs %s`",
			shortID(sess.ID), sess.Status, shortID(sess.ID))
	}
	if e.canResume(sess) {
		return fmt.Errorf("session %s is not reachable; use `rvr resume %s`",
			shortID(sess.ID), shortID(sess.ID))
	}
	return fmt.Errorf("session %s is not reachable; inspect logs with `rvr logs %s`\nSupervisor log: %s",
		shortID(sess.ID), shortID(sess.ID), e.supervisorLogPath(sess.ID))
}

func (e *env) failureSummary(st *store.Store, sess *session.Session) string {
	detail := failureDetail(st, sess)
	if detail == "" {
		detail = "no failure detail recorded"
	}
	return fmt.Sprintf("session %s failed: %s\nSupervisor log: %s",
		shortID(sess.ID), detail, e.supervisorLogPath(sess.ID))
}

func failureDetail(st *store.Store, sess *session.Session) string {
	if detail := strings.TrimSpace(sess.StatusDetail); detail != "" {
		return detail
	}
	events, err := st.ListEvents(sess.ID)
	if err != nil {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "error" {
			continue
		}
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(events[i].Payload), &payload); err == nil {
			if msg := strings.TrimSpace(payload.Message); msg != "" {
				return msg
			}
		}
	}
	return ""
}

func missingHarnessDetail(name string) string {
	return fmt.Sprintf("harness %q is not configured (see rvr config)", name)
}

// spawnSupervisor starts a detached `rvr _supervise <id>` process that
// outlives this one (SPEC.md §3). It returns the supervisor pid.
func (e *env) spawnSupervisor(id string, resume bool) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate rvr binary: %w", err)
	}
	if err := os.MkdirAll(e.paths.LogsDir, 0o700); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(
		filepath.Join(e.paths.LogsDir, id+".supervisor.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600,
	)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer devnull.Close()

	args := []string{"_supervise", id}
	if resume {
		args = append(args, "--resume")
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = withoutEnv(os.Environ(), attach.ResultFDEnv)
	cmd.Stdin = devnull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from our session
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start supervisor: %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

// waitForSocket blocks until the session's supervisor is accepting, or timeout.
func (e *env) waitForSocket(id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	path := e.socketPath(id)
	for time.Now().Before(deadline) {
		if attach.Alive(path) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// deriveTitle produces a session title from an explicit flag or the prompt.
func deriveTitle(explicit, prompt string) string {
	if explicit != "" {
		return explicit
	}
	line := prompt
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	const max = 60
	if len(line) > max {
		return strings.TrimSpace(line[:max]) + "…"
	}
	if line == "" {
		return "untitled session"
	}
	return line
}

// gitBranch returns the current branch of repo, or "" if not a git repo.
func gitBranch(repo string) string {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
