package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

type completionCandidate struct {
	value string
	desc  string
}

func TestSessionIDCompletion(t *testing.T) {
	setupCompletionEnv(t)
	seedCompletionSessions(t,
		&session.Session{
			ID:       "aaaa1111-0000-0000-0000-000000000001",
			Title:    "alpha task",
			RepoPath: t.TempDir(),
			Harness:  "opencode",
			Status:   session.StatusRunning,
		},
		&session.Session{
			ID:       "bbbb2222-0000-0000-0000-000000000002",
			Title:    "beta task",
			RepoPath: t.TempDir(),
			Harness:  "codex",
			Status:   session.StatusFailed,
		},
	)

	for _, name := range []string{"attach", "resume", "kill", "logs"} {
		t.Run(name, func(t *testing.T) {
			candidates, directive := complete(t, "__complete", name, "")
			values := completionValues(candidates)
			if !slices.Contains(values, "aaaa1111") || !slices.Contains(values, "bbbb2222") {
				t.Fatalf("%s completions = %#v, want seeded short IDs", name, values)
			}
			descs := completionDescriptions(candidates)
			if descs["aaaa1111"] != "running - alpha task" {
				t.Fatalf("%s completion description = %q, want status and title", name, descs["aaaa1111"])
			}
			if directive != noFileDirective() {
				t.Fatalf("%s directive = %q, want %q", name, directive, noFileDirective())
			}
		})
	}
}

func TestSessionIDCompletionFiltersPrefix(t *testing.T) {
	setupCompletionEnv(t)
	seedCompletionSessions(t,
		&session.Session{ID: "aaaa1111-0000-0000-0000-000000000001", Title: "alpha", RepoPath: t.TempDir(), Harness: "opencode", Status: session.StatusRunning},
		&session.Session{ID: "bbbb2222-0000-0000-0000-000000000002", Title: "beta", RepoPath: t.TempDir(), Harness: "codex", Status: session.StatusRunning},
	)

	candidates, directive := complete(t, "__complete", "attach", "bbbb")
	values := completionValues(candidates)
	if !slices.Equal(values, []string{"bbbb2222"}) {
		t.Fatalf("filtered completions = %#v, want only bbbb2222", values)
	}
	if directive != noFileDirective() {
		t.Fatalf("directive = %q, want %q", directive, noFileDirective())
	}
}

func TestSessionIDCompletionNoSecondArg(t *testing.T) {
	setupCompletionEnv(t)
	seedCompletionSessions(t,
		&session.Session{ID: "aaaa1111-0000-0000-0000-000000000001", Title: "alpha", RepoPath: t.TempDir(), Harness: "opencode", Status: session.StatusRunning},
	)

	candidates, directive := complete(t, "__complete", "attach", "aaaa1111", "")
	if len(candidates) != 0 {
		t.Fatalf("second-arg completions = %#v, want none", completionValues(candidates))
	}
	if directive != noFileDirective() {
		t.Fatalf("directive = %q, want %q", directive, noFileDirective())
	}
}

func TestSessionIDCompletionKeepsAmbiguousShortIDsUnique(t *testing.T) {
	setupCompletionEnv(t)
	seedCompletionSessions(t,
		&session.Session{ID: "deadbeef-0000-0000-0000-000000000001", Title: "first", RepoPath: t.TempDir(), Harness: "opencode", Status: session.StatusRunning},
		&session.Session{ID: "deadbeef-1111-0000-0000-000000000002", Title: "second", RepoPath: t.TempDir(), Harness: "codex", Status: session.StatusRunning},
	)

	candidates, _ := complete(t, "__complete", "attach", "dead")
	values := completionValues(candidates)
	if !slices.Equal(values, []string{"deadbeef-1", "deadbeef-0"}) &&
		!slices.Equal(values, []string{"deadbeef-0", "deadbeef-1"}) {
		t.Fatalf("ambiguous short completions = %#v, want unique prefixes", values)
	}
}

func TestHarnessFlagCompletion(t *testing.T) {
	paths := setupCompletionEnv(t)
	writeCompletionConfig(t, paths, `
[harness.goose]
adapter = "generic"
command = "goose"
`)

	candidates, directive := complete(t, "__complete", "new", "--harness", "")
	values := completionValues(candidates)
	for _, want := range []string{"codex", "goose", "opencode", "pi"} {
		if !slices.Contains(values, want) {
			t.Fatalf("harness completions = %#v, missing %q", values, want)
		}
	}
	descs := completionDescriptions(candidates)
	if descs["opencode"] != "default" {
		t.Fatalf("opencode description = %q, want default", descs["opencode"])
	}
	if descs["goose"] != "generic" {
		t.Fatalf("goose description = %q, want generic", descs["goose"])
	}
	if directive != noFileDirective() {
		t.Fatalf("directive = %q, want %q", directive, noFileDirective())
	}
}

func TestSessionIDCompletionWithoutStoreDoesNotCreateDB(t *testing.T) {
	paths := setupCompletionEnv(t)

	candidates, directive := complete(t, "__complete", "attach", "")
	if len(candidates) != 0 {
		t.Fatalf("completions without DB = %#v, want none", completionValues(candidates))
	}
	if directive != noFileDirective() {
		t.Fatalf("directive = %q, want %q", directive, noFileDirective())
	}
	if _, err := os.Stat(paths.DBFile); !os.IsNotExist(err) {
		t.Fatalf("completion created DB at %s; stat err=%v", paths.DBFile, err)
	}
}

func complete(t *testing.T, args ...string) ([]completionCandidate, string) {
	t.Helper()
	stdout, stderr, err := executeCommand(newRootCmd(), args...)
	if err != nil {
		t.Fatalf("complete %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
	}
	if !strings.Contains(stderr, "ShellCompDirectiveNoFileComp") {
		t.Fatalf("completion stderr = %q, want no-file directive", stderr)
	}

	var candidates []completionCandidate
	var directive string
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			directive = strings.TrimPrefix(line, ":")
			continue
		}
		value, desc, _ := strings.Cut(line, "\t")
		candidates = append(candidates, completionCandidate{value: value, desc: desc})
	}
	return candidates, directive
}

func executeCommand(root *cobra.Command, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func setupCompletionEnv(t *testing.T) config.Paths {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func seedCompletionSessions(t *testing.T, sessions ...*session.Session) {
	t.Helper()
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, sess := range sessions {
		if err := st.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCompletionConfig(t *testing.T, paths config.Paths, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func completionValues(candidates []completionCandidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.value)
	}
	return values
}

func completionDescriptions(candidates []completionCandidate) map[string]string {
	descs := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		descs[candidate.value] = candidate.desc
	}
	return descs
}

func noFileDirective() string {
	return fmt.Sprint(cobra.ShellCompDirectiveNoFileComp)
}
