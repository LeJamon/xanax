// Package cli wires the rvr command tree (SPEC.md §9).
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/tui"
)

// version is injected by release builds. Go-installed builds fall back to the
// module version embedded by the toolchain; source builds report 0.1.0-dev.
var version = "0.1.0-dev"

func resolvedVersion() string {
	moduleVersion := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = info.Main.Version
	}
	return selectVersion(version, moduleVersion)
}

func selectVersion(injected, moduleVersion string) string {
	if injected != "" && injected != "0.1.0-dev" {
		return strings.TrimPrefix(injected, "v")
	}
	if moduleVersion != "" && moduleVersion != "(devel)" {
		return strings.TrimPrefix(moduleVersion, "v")
	}
	return "0.1.0-dev"
}

// Execute runs the root command and returns its error for main to report.
func Execute() error {
	attach.ProtectResultFD()
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	release := resolvedVersion()
	root := &cobra.Command{
		Use:   "rvr [path]",
		Short: "Session manager for autonomous AI coding agents",
		Long: `rvr launches, supervises, and reattaches to autonomous AI coding agent
sessions (opencode, pi, ...) so they keep running when your terminal doesn't.

With no argument the dashboard shows every session. Given a path, it scopes to
sessions whose repository is under that path and launches new ones there.`,
		Version:                    release,
		Args:                       cobra.MaximumNArgs(1),
		SuggestionsMinimumDistance: 2,
		SilenceUsage:               true,
		SilenceErrors:              true,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := resolveRootScopeArg(cmd, args)
			if err != nil {
				return err
			}
			return runDashboard(scope)
		},
	}
	root.AddCommand(
		newNewCmd(),
		newListCmd(),
		newAttachCmd(),
		newResumeCmd(),
		newKillCmd(),
		newRmCmd(),
		newPruneCmd(),
		newLogsCmd(),
		newConfigCmd(),
		newSuperviseCmd(),
	)
	return root
}

func resolveRootScopeArg(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	scope := args[0]
	fi, err := os.Stat(scope)
	switch {
	case err == nil && fi.IsDir():
		return scope, nil
	case err == nil:
		return "", fmt.Errorf("scope %q is not a directory", scope)
	case os.IsNotExist(err):
		return "", unknownRootCommandError(cmd, scope)
	default:
		return "", fmt.Errorf("scope %q is not a directory: %w", scope, err)
	}
}

func unknownRootCommandError(cmd *cobra.Command, arg string) error {
	msg := fmt.Sprintf("unknown command %q for %q", arg, cmd.CommandPath())
	suggestions := cmd.SuggestionsFor(arg)
	if len(suggestions) == 0 {
		return fmt.Errorf("%s", msg)
	}
	if len(suggestions) == 1 {
		return fmt.Errorf("%s\n\nDid you mean this?\n\t%s", msg, suggestions[0])
	}
	return fmt.Errorf("%s\n\nDid you mean one of these?\n\t%s", msg, strings.Join(suggestions, "\n\t"))
}

// env bundles the resolved paths and configuration every command needs.
type env struct {
	paths   config.Paths
	cfg     *config.Config
	nowFn   func() time.Time
	aliveFn func(string) bool
}

func loadEnv() (*env, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	return &env{paths: paths, cfg: cfg}, nil
}

func (e *env) openStore() (*store.Store, error) {
	return store.Open(e.paths.DBFile)
}

// runDashboard reconciles interrupted sessions (auto-resume) and launches the
// dashboard TUI. A non-empty scope restricts it to sessions under that path.
func runDashboard(scope string) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}
	if scope != "" {
		abs, err := filepath.Abs(scope)
		if err != nil {
			return err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return fmt.Errorf("scope %q is not a directory", scope)
		}
		scope = abs
	}

	st, err := e.openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if resumed, err := e.reconcile(st); err == nil && len(resumed) > 0 {
		// Give freshly auto-resumed supervisors a moment to bind their sockets
		// before the first render so they show as running, not interrupted.
		e.waitForSocket(resumed[0], 3*time.Second)
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	return tui.Run(tui.Deps{
		Store:      st,
		Cfg:        e.cfg,
		SelfPath:   self,
		LogsDir:    e.paths.LogsDir,
		SocketDir:  e.paths.SocketDir,
		ConfigPath: e.paths.ConfigFile,
		Scope:      scope,
		Version:    resolvedVersion(),
		Reconcile:  func() error { return e.reconcileLatestConfig(st) },
	})
}

func (e *env) reconcileLatestConfig(st *store.Store) error {
	cfg, err := config.Load(e.paths.ConfigFile)
	if err != nil {
		return err
	}
	current := *e
	current.cfg = cfg
	_, err = current.reconcile(st)
	return err
}
