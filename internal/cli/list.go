package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/session"
)

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "ps"},
		Short:   "List sessions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}
			st, err := e.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			sessions, err := st.ListSessions()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if sessions == nil {
					_, err := fmt.Fprintln(out, "[]")
					return err
				}
				return enc.Encode(sessions)
			}
			if len(sessions) == 0 {
				fmt.Fprintln(out, "No sessions. Start one with: rvr new \"your prompt\"")
				return nil
			}
			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tEXIT\tHARNESS\tAGE\tREPO\tTITLE\tDETAIL")
			for _, s := range sessions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					shortID(s.ID), s.Status, listExitCode(s), s.Harness,
					humanAge(s.CreatedAt), filepath.Base(s.RepoPath), s.Title, listDetail(s))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output sessions as JSON")
	return cmd
}

func listExitCode(s *session.Session) string {
	if s.ExitCode == nil {
		return "-"
	}
	return strconv.Itoa(*s.ExitCode)
}

func listDetail(s *session.Session) string {
	if detail := strings.TrimSpace(s.StatusDetail); detail != "" {
		return detail
	}
	return "-"
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
