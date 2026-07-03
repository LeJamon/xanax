package tui

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// gitInfo is the live VCS context for a repo, shown right-aligned on its rows.
type gitInfo struct {
	branch string
	pr     string // PR number for branch, or "" (best-effort via gh)
}

type gitInfoMsg struct {
	infos    map[string]gitInfo
	polledPR bool // whether this poll refreshed PR numbers (else preserve cached)
}

// gitPollCmd resolves the current branch of each repo, and (when polledPR) the
// open PR for that branch. Runs in a goroutine; all lookups are best-effort.
func gitPollCmd(repos []string, pollPR bool) tea.Cmd {
	return func() tea.Msg {
		infos := make(map[string]gitInfo, len(repos))
		for _, repo := range repos {
			gi := gitInfo{branch: gitBranchOf(repo)}
			if pollPR && gi.branch != "" {
				gi.pr = ghPRNumber(repo, gi.branch)
			}
			infos[repo] = gi
		}
		return gitInfoMsg{infos: infos, polledPR: pollPR}
	}
}

// gitBranchOf returns the current branch, or "" if repo is not a git repo or
// is in detached-HEAD state.
func gitBranchOf(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	b := strings.TrimSpace(string(out))
	if b == "HEAD" { // detached
		return ""
	}
	return b
}

// ghPRNumber returns the open PR number for branch via the gh CLI, or "" when
// gh is missing/unauthenticated or there is no PR.
func ghPRNumber(repo, branch string) string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "open", "--json", "number", "--limit", "1")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var prs []struct {
		Number int `json:"number"`
	}
	if json.Unmarshal(out, &prs) != nil || len(prs) == 0 {
		return ""
	}
	return strconv.Itoa(prs[0].Number)
}

// uniqueRepos collects the distinct repo paths among the sessions.
func uniqueRepos(m model) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range m.sessions {
		if !seen[s.RepoPath] {
			seen[s.RepoPath] = true
			out = append(out, s.RepoPath)
		}
	}
	return out
}
