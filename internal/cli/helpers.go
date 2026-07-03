package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"xanax/internal/attach"
	"xanax/internal/session"
)

// socketPath is the unix socket for a session's supervisor.
func (e *env) socketPath(id string) string {
	return filepath.Join(e.paths.SocketDir, id+".sock")
}

// canResume reports whether a dead session can be relaunched: either a
// harness-native session ref was captured (pi/opencode), or its harness has
// resume_args configured (generic "continue last session in this repo").
func (e *env) canResume(sess *session.Session) bool {
	if sess.HarnessSessionRef != "" {
		return true
	}
	h, ok := e.cfg.Harnesses[sess.Harness]
	return ok && len(h.ResumeArgs) > 0
}

// spawnSupervisor starts a detached `xanax _supervise <id>` process that
// outlives this one (SPEC.md §3). It returns the supervisor pid.
func (e *env) spawnSupervisor(id string, resume bool) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate xanax binary: %w", err)
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
