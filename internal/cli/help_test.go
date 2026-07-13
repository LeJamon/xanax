package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInteractiveCommandHelpMentionsDetachKey(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "new", cmd: newNewCmd()},
		{name: "attach", cmd: newAttachCmd()},
		{name: "resume", cmd: newResumeCmd()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			help := commandHelp(t, tt.cmd)
			for _, want := range []string{"configured detach key", "ctrl+q", "keeps running"} {
				if !strings.Contains(help, want) {
					t.Fatalf("%s help missing %q:\n%s", tt.name, want, help)
				}
			}
			for _, stale := range []string{"Left arrow", `ctrl+\`} {
				if strings.Contains(help, stale) {
					t.Fatalf("%s help contains stale detach key %q:\n%s", tt.name, stale, help)
				}
			}
		})
	}
}

func commandHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help: %v", err)
	}
	return buf.String()
}
