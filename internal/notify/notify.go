// Package notify sends best-effort desktop notifications on macOS and Linux.
// A notification failure is never fatal — it is a convenience, not a
// correctness feature.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Send posts a desktop notification. It returns nil on unsupported platforms
// or when the platform tool is missing, so callers can ignore the error.
func Send(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return run("osascript", osascriptArgs(title, body))
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return nil
		}
		return run("notify-send", []string{title, body})
	default:
		return nil
	}
}

func run(bin string, args []string) error {
	if _, err := exec.LookPath(bin); err != nil {
		return nil
	}
	return exec.Command(bin, args...).Run()
}

// osascriptArgs builds the `osascript -e <script>` arguments. AppleScript
// string literals are double-quoted with backslash and quote escaped.
func osascriptArgs(title, body string) []string {
	script := "display notification " + appleQuote(body) + " with title " + appleQuote(title)
	return []string{"-e", script}
}

func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Newlines would terminate the AppleScript line; collapse them.
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}
