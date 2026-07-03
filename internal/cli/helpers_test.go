package cli

import (
	"testing"

	"xanax/internal/config"
	"xanax/internal/session"
)

func TestCanResume(t *testing.T) {
	e := &env{cfg: &config.Config{Harnesses: map[string]config.Harness{
		"opencode": {Adapter: "opencode"},                            // no resume_args
		"goose":    {Adapter: "generic", ResumeArgs: []string{"-c"}}, // continue flag
		"aider":    {Adapter: "generic"},                             // nothing
	}}}

	cases := []struct {
		name string
		sess *session.Session
		want bool
	}{
		{"captured ref always resumable", &session.Session{Harness: "opencode", HarnessSessionRef: "ses_1"}, true},
		{"generic with resume_args", &session.Session{Harness: "goose"}, true},
		{"generic without resume_args", &session.Session{Harness: "aider"}, false},
		{"native without ref", &session.Session{Harness: "opencode"}, false},
		{"unknown harness", &session.Session{Harness: "gone"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := e.canResume(c.sess); got != c.want {
				t.Errorf("canResume = %v, want %v", got, c.want)
			}
		})
	}
}
