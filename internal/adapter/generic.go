package adapter

import (
	"io"
	"os"
	"sort"

	"xanax/internal/config"
	"xanax/internal/session"
)

// genericAdapter runs any CLI from configuration. It has no state channel, so
// its sessions report only running and terminal states (SPEC.md §5). The
// initial prompt is delivered on the command line when the harness declares how
// (prompt_arg / prompt_positional), else typed into the PTY as a fallback.
type genericAdapter struct {
	sess *session.Session
	h    config.Harness
	// resuming records whether the most recent Launch was a resume, so the PTY
	// fallback in AfterStart does not re-type the initial prompt into a session
	// that is only being reattached. Launch always runs before AfterStart.
	resuming bool
}

func newGeneric(sess *session.Session, h config.Harness, _ Deps) (Adapter, error) {
	return &genericAdapter{sess: sess, h: h}, nil
}

// promptOnCmdline reports whether the prompt is delivered as an argument (so it
// must not also be typed into the PTY).
func (a *genericAdapter) promptOnCmdline() bool {
	return a.h.PromptArg != "" || a.h.PromptPositional
}

func (a *genericAdapter) Launch(resume bool) (LaunchSpec, error) {
	a.resuming = resume
	args := append([]string(nil), a.h.Args...)
	switch {
	case resume && len(a.h.ResumeArgs) > 0:
		args = append([]string(nil), a.h.ResumeArgs...)
	case !resume && a.sess.InitialPrompt != "" && a.h.PromptArg != "":
		args = append(args, a.h.PromptArg, a.sess.InitialPrompt)
	case !resume && a.sess.InitialPrompt != "" && a.h.PromptPositional:
		args = append(args, a.sess.InitialPrompt)
	}
	return LaunchSpec{
		Path: a.h.Command,
		Args: args,
		Env:  mergeEnv(a.h.Env),
		Dir:  a.sess.RepoPath,
	}, nil
}

func (a *genericAdapter) AfterStart(pty io.Writer) error {
	if a.sess.InitialPrompt == "" || a.promptOnCmdline() || a.resuming {
		return nil // no prompt, already delivered on the command line, or resuming
	}
	// Fallback: type the prompt followed by Enter. Line-based CLIs consume it;
	// full-screen TUIs are better served by prompt_arg/prompt_positional or a
	// native adapter (typing races the TUI's startup).
	_, err := io.WriteString(pty, a.sess.InitialPrompt+"\r")
	return err
}

func (a *genericAdapter) States() <-chan StateEvent { return nil }
func (a *genericAdapter) SessionRef() string        { return "" }
func (a *genericAdapter) Close() error              { return nil }

// mergeEnv overlays the harness's configured env onto the current process
// environment. A nil/empty overlay returns nil so the child inherits as-is.
func mergeEnv(overlay map[string]string) []string {
	if len(overlay) == 0 {
		return nil
	}
	base := os.Environ()
	keys := make([]string, 0, len(overlay))
	for k := range overlay {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order for tests
	for _, k := range keys {
		base = append(base, k+"="+overlay[k])
	}
	return base
}
