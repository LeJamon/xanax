package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"xanax/internal/session"
)

func completeSessionIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	e, err := loadEnv()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if _, err := os.Stat(e.paths.DBFile); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	st, err := e.openStore()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer st.Close()

	sessions, err := st.ListSessions()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		ids = append(ids, sess.ID)
	}

	completions := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if !strings.HasPrefix(sess.ID, toComplete) {
			continue
		}
		id := completionID(sess.ID, ids, toComplete)
		completions = append(completions, cobra.CompletionWithDesc(id, sessionCompletionDescription(sess)))
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeHarnessNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	e, err := loadEnv()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(e.cfg.Harnesses))
	for name := range e.cfg.Harnesses {
		if strings.HasPrefix(name, toComplete) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	completions := make([]string, 0, len(names))
	for _, name := range names {
		desc := e.cfg.Harnesses[name].Adapter
		if name == e.cfg.DefaultHarness {
			desc = "default"
		}
		completions = append(completions, cobra.CompletionWithDesc(name, desc))
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completionID(id string, all []string, toComplete string) string {
	want := 8
	if len(toComplete) > want {
		want = len(toComplete)
	}
	if len(id) < want {
		want = len(id)
	}
	for want < len(id) && !uniquePrefix(id, all, want) {
		want++
	}
	return id[:want]
}

func uniquePrefix(id string, all []string, n int) bool {
	prefix := id[:n]
	for _, other := range all {
		if other != id && strings.HasPrefix(other, prefix) {
			return false
		}
	}
	return true
}

func sessionCompletionDescription(sess *session.Session) string {
	title := compactCompletionText(sess.Title)
	if title == "" {
		return string(sess.Status)
	}
	return fmt.Sprintf("%s - %s", sess.Status, title)
}

func compactCompletionText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
