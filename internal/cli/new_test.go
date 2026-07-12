package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

func TestNewJSONOutputsCreatedSession(t *testing.T) {
	paths := setupNewCommandEnv(t)
	calls := stubNewSessionSupervisor(t)
	repo := t.TempDir()

	stdout, stderr, err := executeCLI(newRootCmd(),
		"new", "--json", "--repo", repo, "--harness", "opencode", "build the API")
	if err != nil {
		t.Fatalf("new --json failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if strings.Contains(stdout, "Started session") || strings.Contains(stdout, "Attach with") {
		t.Fatalf("stdout contains prose instead of only JSON: %q", stdout)
	}

	var got session.Session
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a session JSON object: %v\n%s", err, stdout)
	}
	if got.ID == "" {
		t.Fatal("JSON session ID is empty")
	}
	if got.Title != "build the API" || got.InitialPrompt != "build the API" {
		t.Fatalf("JSON title/prompt = %q/%q, want prompt-derived title and prompt", got.Title, got.InitialPrompt)
	}
	if got.RepoPath != repo {
		t.Fatalf("JSON repo_path = %q, want %q", got.RepoPath, repo)
	}
	if got.Harness != "opencode" || got.Status != session.StatusStarting {
		t.Fatalf("JSON harness/status = %q/%q, want opencode/starting", got.Harness, got.Status)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("JSON timestamps were not populated: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
	if len(*calls) != 1 || (*calls)[0] != got.ID {
		t.Fatalf("supervisor calls = %#v, want exactly created ID %q", *calls, got.ID)
	}

	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stored, err := st.GetSession(got.ID)
	if err != nil {
		t.Fatalf("stored session %q not found: %v", got.ID, err)
	}
	if stored.ID != got.ID || stored.InitialPrompt != got.InitialPrompt || stored.Title != got.Title {
		t.Fatalf("stored session = %#v, want JSON session %#v", stored, got)
	}
}

func TestNewNoAttachKeepsHumanOutput(t *testing.T) {
	setupNewCommandEnv(t)
	stubNewSessionSupervisor(t)
	repo := t.TempDir()

	stdout, stderr, err := executeCLI(newRootCmd(),
		"new", "--no-attach", "--repo", repo, "--harness", "opencode", "human output")
	if err != nil {
		t.Fatalf("new --no-attach failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Started session ") || !strings.Contains(stdout, "Attach with: xanax attach ") {
		t.Fatalf("stdout = %q, want existing prose output", stdout)
	}
	if json.Valid([]byte(stdout)) {
		t.Fatalf("human output unexpectedly became JSON: %q", stdout)
	}
}

func executeCLI(root *cobra.Command, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func setupNewCommandEnv(t *testing.T) config.Paths {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	fakeHarness := filepath.Join(tmp, "fake-opencode")
	if err := os.WriteFile(fakeHarness, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("[harness.opencode]\ncommand = %q\n", fakeHarness)
	if err := os.WriteFile(paths.ConfigFile, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}

func stubNewSessionSupervisor(t *testing.T) *[]string {
	t.Helper()
	orig := startNewSessionSupervisor
	var calls []string
	startNewSessionSupervisor = func(e *env, id string) (int, error) {
		calls = append(calls, id)
		return 1234, nil
	}
	t.Cleanup(func() {
		startNewSessionSupervisor = orig
	})
	return &calls
}
