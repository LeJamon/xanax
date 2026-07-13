package adapter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	sockDir, err := os.MkdirTemp("/tmp", "xnxA")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	return Deps{Paths: config.Paths{DataDir: t.TempDir(), SocketDir: sockDir}}
}

func argIndex(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func hasEnvKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func TestGenericLaunch(t *testing.T) {
	sess := &session.Session{ID: "g1", RepoPath: "/repo", InitialPrompt: "hi"}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "goose", Args: []string{"session"}, ResumeArgs: []string{"session", "--resume"}}
	a, err := New(sess, h, testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	spec, err := a.Launch(false)
	if err != nil {
		t.Fatal(err)
	}
	// No prompt-delivery config → prompt is not on the command line.
	if spec.Path != "goose" || len(spec.Args) != 1 || spec.Args[0] != "session" || spec.Dir != "/repo" {
		t.Errorf("unexpected launch spec: %+v", spec)
	}
	resume, _ := a.Launch(true)
	if len(resume.Args) != 2 || resume.Args[1] != "--resume" {
		t.Errorf("resume args = %v", resume.Args)
	}
}

func TestGenericPromptDelivery(t *testing.T) {
	// prompt_arg: appended as a flag value.
	sess := &session.Session{ID: "g2", RepoPath: "/r", InitialPrompt: "do the thing"}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "goose", Args: []string{"run"}, PromptArg: "--message"}
	a, _ := New(sess, h, testDeps(t))
	defer a.Close()
	spec, _ := a.Launch(false)
	if i := argIndex(spec.Args, "--message"); i < 0 || spec.Args[i+1] != "do the thing" {
		t.Errorf("prompt_arg not applied: %v", spec.Args)
	}

	// prompt_positional: appended as the last argument.
	h2 := config.Harness{Adapter: config.AdapterGeneric, Command: "aider", PromptPositional: true}
	a2, _ := New(&session.Session{ID: "g3", RepoPath: "/r", InitialPrompt: "fix it"}, h2, testDeps(t))
	defer a2.Close()
	spec2, _ := a2.Launch(false)
	if len(spec2.Args) == 0 || spec2.Args[len(spec2.Args)-1] != "fix it" {
		t.Errorf("prompt_positional not applied: %v", spec2.Args)
	}

	// On resume, the prompt is never re-sent.
	sess.HarnessSessionRef = ""
	h.ResumeArgs = []string{"--continue"}
	a3, _ := New(sess, h, testDeps(t))
	defer a3.Close()
	spec3, _ := a3.Launch(true)
	if argIndex(spec3.Args, "--message") >= 0 {
		t.Errorf("resume should not carry the prompt: %v", spec3.Args)
	}
}

func TestGenericDoesNotMutateConfigArgs(t *testing.T) {
	shared := []string{"run"}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "x", Args: shared, PromptArg: "-m"}
	a, _ := New(&session.Session{ID: "g4", RepoPath: "/r", InitialPrompt: "p"}, h, testDeps(t))
	defer a.Close()
	a.Launch(false)
	if len(shared) != 1 {
		t.Errorf("Launch mutated the shared config Args slice: %v", shared)
	}
}

// A PTY-delivery generic harness (no prompt_arg/prompt_positional) must type
// the prompt on a fresh launch but must NOT re-type it on resume — otherwise
// reattaching a resumable generic session re-runs its initial instruction.
func TestGenericAfterStartResume(t *testing.T) {
	sess := &session.Session{ID: "g5", RepoPath: "/r", InitialPrompt: "type me"}
	h := config.Harness{Adapter: config.AdapterGeneric, Command: "aider", ResumeArgs: []string{"--restore"}}

	// Fresh launch: the PTY fallback types the prompt followed by Enter.
	a, _ := New(sess, h, testDeps(t))
	defer a.Close()
	a.Launch(false)
	var fresh bytes.Buffer
	if err := a.AfterStart(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.String() != "type me\r" {
		t.Errorf("fresh AfterStart should type the prompt, got %q", fresh.String())
	}

	// Resume: the prompt must not be re-typed into the PTY.
	a2, _ := New(sess, h, testDeps(t))
	defer a2.Close()
	a2.Launch(true)
	var resumed bytes.Buffer
	if err := a2.AfterStart(&resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.Len() != 0 {
		t.Errorf("resume AfterStart must not re-type the prompt, got %q", resumed.String())
	}
}

// TestGenericCodexLaunch pins the codex generic-harness contract documented in
// README/SPEC: fresh launch is `codex "<prompt>"` (positional, never typed into
// the full-screen TUI), and resume is `codex resume --last` without the prompt.
func TestGenericCodexLaunch(t *testing.T) {
	sess := &session.Session{ID: "cx", RepoPath: "/repo", InitialPrompt: "fix the flaky test"}
	h := config.Harness{
		Adapter:          config.AdapterGeneric,
		Command:          "codex",
		PromptPositional: true,
		ResumeArgs:       []string{"resume", "--last"},
	}
	a, err := New(sess, h, testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	fresh, err := a.Launch(false)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Path != "codex" || len(fresh.Args) != 1 || fresh.Args[0] != "fix the flaky test" {
		t.Errorf("fresh launch = %s %v, want codex [\"fix the flaky test\"]", fresh.Path, fresh.Args)
	}
	// prompt_positional delivers on the command line, so nothing is typed into
	// codex's TUI (which would race its startup).
	var typed bytes.Buffer
	if err := a.AfterStart(&typed); err != nil {
		t.Fatal(err)
	}
	if typed.Len() != 0 {
		t.Errorf("AfterStart typed into the codex TUI: %q", typed.String())
	}

	resume, err := a.Launch(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Args) != 2 || resume.Args[0] != "resume" || resume.Args[1] != "--last" {
		t.Errorf("resume args = %v, want [resume --last]", resume.Args)
	}
	if argIndex(resume.Args, "fix the flaky test") >= 0 {
		t.Errorf("resume must not carry the prompt: %v", resume.Args)
	}
}

func TestOpencodeLaunchHasNoServerPassword(t *testing.T) {
	sess := &session.Session{ID: "o1", RepoPath: "/repo", InitialPrompt: "fix tests"}
	h := config.Harness{Adapter: config.AdapterOpencode, Command: "opencode"}
	a, err := New(sess, h, testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	spec, err := a.Launch(false)
	if err != nil {
		t.Fatal(err)
	}
	// Setting OPENCODE_SERVER_PASSWORD crashes the opencode TUI (its own client
	// 401s), so it must never appear in the launch environment.
	if hasEnvKey(spec.Env, "OPENCODE_SERVER_PASSWORD") {
		t.Error("launch env must not set OPENCODE_SERVER_PASSWORD")
	}
	if argIndex(spec.Args, "--port") < 0 {
		t.Error("expected --port in args")
	}
	if i := argIndex(spec.Args, "--prompt"); i < 0 || spec.Args[i+1] != "fix tests" {
		t.Errorf("expected --prompt with the initial prompt, got %v", spec.Args)
	}
}

func TestOpencodeResumeUsesSessionFlag(t *testing.T) {
	sess := &session.Session{ID: "o2", RepoPath: "/repo", InitialPrompt: "x", HarnessSessionRef: "ses_abc"}
	h := config.Harness{Adapter: config.AdapterOpencode, Command: "opencode"}
	a, _ := New(sess, h, testDeps(t))
	defer a.Close()

	spec, _ := a.Launch(true)
	if i := argIndex(spec.Args, "--session"); i < 0 || spec.Args[i+1] != "ses_abc" {
		t.Errorf("resume should pass --session ses_abc, got %v", spec.Args)
	}
	if argIndex(spec.Args, "--prompt") >= 0 {
		t.Error("resume must not re-send --prompt")
	}
}

func TestPiLaunch(t *testing.T) {
	deps := testDeps(t)
	sess := &session.Session{ID: "p1", RepoPath: "/repo", InitialPrompt: "add pagination"}
	h := config.Harness{Adapter: config.AdapterPi, Command: "pi"}
	a, err := New(sess, h, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	spec, err := a.Launch(false)
	if err != nil {
		t.Fatal(err)
	}
	if i := argIndex(spec.Args, "-e"); i < 0 || filepath.Base(spec.Args[i+1]) != "hook.mjs" {
		t.Errorf("expected -e <hook.mjs>, got %v", spec.Args)
	}
	if argIndex(spec.Args, "add pagination") < 0 {
		t.Errorf("prompt not passed as argv: %v", spec.Args)
	}
	if !hasEnvKey(spec.Env, "PI_SKIP_VERSION_CHECK") || !hasEnvKey(spec.Env, "RVR_HOOK_SOCKET") {
		t.Errorf("pi env missing expected keys: %v", spec.Env)
	}
	// The embedded hook must have been materialized.
	if _, err := os.Stat(filepath.Join(deps.Paths.DataDir, "pi", "hook.mjs")); err != nil {
		t.Errorf("hook not materialized: %v", err)
	}
}
