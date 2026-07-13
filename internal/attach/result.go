package attach

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ResultFDEnv names an inherited file descriptor used by the dashboard's child
// command to report why terminal attachment ended. It is internal process IPC;
// standalone commands do not set it.
const ResultFDEnv = "RVR_ATTACH_RESULT_FD"

// ProtectResultFD keeps the dashboard result pipe available to this process
// while preventing any harness or supervisor it starts from inheriting the
// writer and delaying EOF indefinitely.
func ProtectResultFD() {
	if fd, ok := resultFD(); ok {
		syscall.CloseOnExec(fd)
	}
}

// ReportResult writes the attach outcome to the inherited dashboard pipe when
// one is configured. Reporting is best-effort and never changes CLI behavior.
func ReportResult(result Result) {
	fd, ok := resultFD()
	if !ok {
		return
	}
	f := os.NewFile(uintptr(fd), "rvr-attach-result")
	if f == nil {
		return
	}
	_, _ = f.WriteString(resultToken(result))
	_ = f.Close()
}

func resultFD() (int, bool) {
	raw := os.Getenv(ResultFDEnv)
	if raw == "" {
		return 0, false
	}
	fd, err := strconv.Atoi(raw)
	return fd, err == nil && fd >= 3
}

// ParseResult decodes a result reported by ReportResult.
func ParseResult(p []byte) (Result, bool) {
	switch strings.TrimSpace(string(p)) {
	case "detached":
		return Detached, true
	case "session-exited":
		return SessionExited, true
	case "disconnected":
		return Disconnected, true
	default:
		return Disconnected, false
	}
}

func resultToken(result Result) string {
	switch result {
	case Detached:
		return "detached"
	case SessionExited:
		return "session-exited"
	case Disconnected:
		return "disconnected"
	default:
		return ""
	}
}
