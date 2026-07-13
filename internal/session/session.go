// Package session defines the domain types shared by the CLI, the
// supervisor, and the store.
package session

import "time"

// Status is the lifecycle state of a session (SPEC.md §6).
type Status string

const (
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusIdle      Status = "idle"
	StatusWaiting   Status = "waiting"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Live reports whether the session is expected to have a running supervisor.
func (s Status) Live() bool {
	switch s {
	case StatusStarting, StatusRunning, StatusIdle, StatusWaiting:
		return true
	}
	return false
}

// Terminal reports whether the session has reached a final state.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Session mirrors one row of the sessions table (SPEC.md §7).
type Session struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	RepoPath          string     `json:"repo_path"`
	Branch            string     `json:"branch,omitempty"`
	Harness           string     `json:"harness"`
	HarnessSessionRef string     `json:"harness_session_ref,omitempty"`
	InitialPrompt     string     `json:"initial_prompt,omitempty"`
	Status            Status     `json:"status"`
	StatusDetail      string     `json:"status_detail,omitempty"`
	PID               int        `json:"pid,omitempty"`
	SocketPath        string     `json:"socket_path,omitempty"`
	ExitCode          *int       `json:"exit_code,omitempty"`
	Lifecycle         int64      `json:"-"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}
