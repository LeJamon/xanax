package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/store"
)

func TestListShowsFailureDetailAndExitCode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exitCode := 127
	if err := st.CreateSession(&session.Session{
		ID:           "failed-list-session",
		Title:        "launch failed",
		RepoPath:     t.TempDir(),
		Harness:      "opencode",
		Status:       session.StatusFailed,
		StatusDetail: "exec: opencode: not found",
		ExitCode:     &exitCode,
	}); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"EXIT", "DETAIL", "127", "exec: opencode: not found"} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output %q missing %q", got, want)
		}
	}
}
