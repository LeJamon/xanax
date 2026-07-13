package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInteractiveCommandHelpMentionsDetachKeys(t *testing.T) {
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
			for _, want := range []string{"Left arrow", `ctrl+\`, "keeps running"} {
				if !strings.Contains(help, want) {
					t.Fatalf("%s help missing %q:\n%s", tt.name, want, help)
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
