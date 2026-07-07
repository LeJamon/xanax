package cli

import (
	"strings"
	"testing"
)

func TestNewCommandAcceptsFlexiblePromptArgs(t *testing.T) {
	cmd := newNewCmd()
	tests := []struct {
		name string
		args []string
	}{
		{name: "bare new", args: nil},
		{name: "single prompt argument", args: []string{"fix the tests"}},
		{name: "unquoted multiword prompt", args: []string{"fix", "the", "tests"}},
		{name: "stdin marker", args: []string{"-"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cmd.Args(cmd, tt.args); err != nil {
				t.Fatalf("Args(%q) returned error: %v", tt.args, err)
			}
		})
	}
}

func TestPromptFromNewArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{
			name: "bare new",
			want: "",
		},
		{
			name: "quoted prompt",
			args: []string{"fix the tests"},
			want: "fix the tests",
		},
		{
			name: "unquoted multiword prompt",
			args: []string{"fix", "the", "tests"},
			want: "fix the tests",
		},
		{
			name:  "stdin prompt trims one line ending",
			args:  []string{"-"},
			stdin: "line one\nline two\n",
			want:  "line one\nline two",
		},
		{
			name:  "stdin prompt trims windows line ending",
			args:  []string{"-"},
			stdin: "fix the tests\r\n",
			want:  "fix the tests",
		},
		{
			name:  "dash is literal within multiword prompt",
			args:  []string{"fix", "-", "tests"},
			stdin: "ignored",
			want:  "fix - tests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newNewCmd()
			cmd.SetIn(strings.NewReader(tt.stdin))

			got, err := promptFromNewArgs(cmd, tt.args)
			if err != nil {
				t.Fatalf("promptFromNewArgs() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("promptFromNewArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}
