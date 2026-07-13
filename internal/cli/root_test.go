package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectVersion(t *testing.T) {
	tests := []struct {
		injected string
		module   string
		want     string
	}{
		{injected: "v1.2.3", module: "v9.9.9", want: "1.2.3"},
		{injected: "0.1.0-dev", module: "v1.2.3", want: "1.2.3"},
		{injected: "0.1.0-dev", module: "(devel)", want: "0.1.0-dev"},
	}
	for _, tt := range tests {
		if got := selectVersion(tt.injected, tt.module); got != tt.want {
			t.Errorf("selectVersion(%q, %q) = %q, want %q", tt.injected, tt.module, got, tt.want)
		}
	}
}

func TestRootUnknownCommandSuggestsList(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"lst"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want unknown command error")
	}
	got := err.Error()
	for _, want := range []string{
		`unknown command "lst" for "rvr"`,
		"Did you mean this?",
		"\tlist",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "scope") {
		t.Fatalf("error %q still reports the typo as a scope", got)
	}
}

func TestRootUnknownCommandWithoutSuggestion(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"definitely-not-an-rvr-command"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want unknown command error")
	}
	got := err.Error()
	if !strings.Contains(got, `unknown command "definitely-not-an-rvr-command" for "rvr"`) {
		t.Fatalf("error %q does not contain unknown command message", got)
	}
	if strings.Contains(got, "Did you mean") {
		t.Fatalf("error %q includes an unexpected suggestion", got)
	}
	if strings.Contains(got, "scope") {
		t.Fatalf("error %q still reports the typo as a scope", got)
	}
}

func TestResolveRootScopeArgKeepsPathBehavior(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveRootScopeArg(newRootCmd(), []string{dir})
	if err != nil {
		t.Fatalf("resolveRootScopeArg returned error for directory: %v", err)
	}
	if got != dir {
		t.Fatalf("resolveRootScopeArg = %q, want %q", got, dir)
	}

	file := filepath.Join(dir, "scope.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = resolveRootScopeArg(newRootCmd(), []string{file})
	if err == nil {
		t.Fatal("resolveRootScopeArg succeeded for file, want error")
	}
	gotErr := err.Error()
	if !strings.Contains(gotErr, "scope") || !strings.Contains(gotErr, "is not a directory") {
		t.Fatalf("file error = %q, want scope-not-directory error", gotErr)
	}
	if strings.Contains(gotErr, "unknown command") {
		t.Fatalf("file error = %q, want path error not unknown-command error", gotErr)
	}
}

func TestListAliasesExecuteListCommand(t *testing.T) {
	for _, alias := range []string{"ls", "ps"} {
		t.Run(alias, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{alias})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s returned error: %v", alias, err)
			}
			want := "No sessions. Start one with: rvr new \"your prompt\"\n"
			if got := out.String(); got != want {
				t.Fatalf("%s output = %q, want %q", alias, got, want)
			}
		})
	}
}
