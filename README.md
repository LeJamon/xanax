# rvr

A terminal-first session manager for autonomous AI coding agents — `tmux + htop`
for harnesses like **opencode**, **pi**, and **codex**. rvr launches agents,
supervises them so they keep running after you close the UI, and gives you one
dashboard to attach, monitor, and manage them regardless of which harness is
doing the work.

rvr is **not** an agent framework: no planning, prompting, memory, or tools. That
stays the harness's job. See [`SPEC.md`](SPEC.md) for the full v1 design.

## Status

Beta. The core session lifecycle, terminal attach/replay, persistence, native
adapter contracts, and generic PTY path are covered by automated tests on macOS
and Linux. Native harness APIs move quickly; see the compatibility matrix below
for the exact integration and fallback behavior.

## Install

Tagged releases publish checksummed archives for macOS and Linux on amd64 and
arm64 from [GitHub Releases](https://github.com/LeJamon/rvr/releases).

With Go 1.26.5 or newer:

```sh
go install github.com/LeJamon/rvr/cmd/rvr@latest
```

Or build the current checkout:

```sh
go build -o rvr ./cmd/rvr
```

## Usage

```sh
rvr                                    # dashboard: all sessions
rvr ~/code/api                         # dashboard scoped to one path
rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"
printf '%s\n' "long prompt" | rvr new -
rvr list [--json]                      # aliases: ls, ps
rvr attach <id>                        # reattach to a live session
rvr resume <id>                        # reattach, or relaunch a dead one natively
rvr kill   <id>                        # terminate but keep the session record
rvr rm     <id>... [--force]           # remove sessions; --force kills live ones first
rvr prune                              # remove terminal sessions
rvr logs   <id> [-f]                   # print (or follow) a session's raw output
rvr config                             # print resolved config + paths
```

Given a path, the dashboard shows only sessions under it and launches new ones there.

Session IDs accept unique prefixes (git-style).

### Navigation

The sessions and the prompt box form one column. When the prompt is empty,
`↑`/`↓` move the selection; `k`/`j` are vim-style aliases while a session is
selected. While the prompt contains text, `↑`/`↓` move its cursor between rows
without leaving the box, and **Esc** clears the draft. The selected row is framed
with accent-colored top/bottom rules.

- **Prompt box selected** (bottom row): type/paste a prompt and press **Enter** to
  launch a new session in the background (fire off several), or **Ctrl+O** to launch
  **and attach** — you land in the harness's own input with its native `/commands` and
  `@file` syntax (empty prompt = fresh harness). **Tab** opens the harness picker to
  choose which harness the next session uses. You can only type when it's selected.
- **A session selected:** `→`/`Enter` open a live window, or inspect stored logs
  for a finished session · `l` show stored logs · `space` toggle a live peek
  (falling back to stored logs for finished sessions) · `e` rename · `r` resume ·
  `Ctrl+X` remove (`Ctrl+X` again for live sessions) · `/` filter · `↓`/`j` back
  to the prompt box · `Ctrl+C` quit. (Rename is a
  rvr-only label; it never touches the harness's own session.) The peek shows
  the session's current screen in a pane above the prompt and closes when the
  selection moves; each row shows its live git branch (and open PR number, via
  `gh`) on the right.
- **Session window:** opening a session drops you into the harness's own live TUI (the
  conversation). Press **`ctrl+q`** by default to detach — the session keeps running in the
  background; **Right arrow** from the list opens it. Every harness key, including
  arrows, is forwarded while attached.

Sessions survive closing the dashboard and even a reboot: on the next launch, rvr
auto-resumes interrupted sessions via the harness's native resume (like Claude
Code's background agents). Finished sessions do not relaunch on `Enter`; use `r`
or `rvr resume <id>` when you explicitly want to resume one.

## Compatibility

| Harness | Integration | State detection | Resume behavior |
|---|---|---|---|
| opencode | Native adapter, local SSE API | Busy, idle, permission/input, error | Exact captured session ID |
| pi | Native adapter, embedded hook | Agent busy/idle lifecycle | Exact captured session file |
| codex | Generic full-screen adapter | Output pattern plus non-actionable idle timeout | `codex resume --last` |
| Other PTY CLIs | Configured generic adapter | Optional waiting pattern and idle timeout | Configured `resume_args` |

If a native side channel is unavailable or its upstream API changes, the
harness keeps running and rvr degrades to process-level running/exited state.
CI tests adapter contracts and terminal behavior but does not install external
harness binaries; release notes should record any live-tested harness versions.

## Configuration

Optional, at `~/.config/rvr/config.toml`. opencode, pi, and codex work with no
config.

```toml
default_harness   = "opencode"
auto_resume       = true
notifications     = true
interact_exit_key = "ctrl+q"

[harness.opencode]
adapter = "opencode"
command = "opencode"

[harness.pi]
adapter = "pi"
command = "pi"

# codex is built in as a generic full-screen TUI harness. If you override it,
# omitted fields keep these defaults.
[harness.codex]
adapter           = "generic"
command           = "codex"
full_screen       = true                  # attach uses a screen snapshot, not raw replay
prompt_positional = true                  # codex "<prompt>" starts a session with it
resume_args       = ["resume", "--last"]  # reattach to the most recent session
idle_timeout      = 120                   # no native state; mark non-actionable "idle"

# Optional TUI colors — ANSI palette index ("0"–"255") or hex ("#rgb"/"#rrggbb").
# Any omitted field keeps its default; see `rvr config` for the full set.
[theme]
accent    = "13"   # selection / cursor
muted     = "250"  # secondary text and rules
branch    = "6"    # git branch on rows

# Any other PTY CLI works via the generic adapter:
[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]                # start goose's interactive session
resume_args = ["session", "--resume"]    # resume the most recent session
# A CLI that takes the prompt as a flag can set prompt_arg = "--flag"
# (or prompt_positional = true) to skip typing it into the PTY.
# Diff-rendered TUIs can set full_screen = true to attach from a screen snapshot
# instead of raw scrollback replay.

```

## Layout

```
cmd/rvr            entrypoint
internal/cli         cobra commands + dashboard wiring
internal/config      config + XDG paths
internal/session     domain types + state model
internal/store       SQLite (pure-Go), sessions + event log
internal/supervisor  per-session PTY owner, socket server, state machine
internal/wire        framed attach protocol
internal/ringbuf     scrollback ring buffer
internal/attach      raw terminal passthrough client
internal/adapter     harness drivers: generic, pi, opencode
internal/tui         Bubble Tea dashboard
```

## Development

```sh
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Release tags matching `v*` are built through GoReleaser. Run `goreleaser check`
before changing release configuration.

## License

[MIT](LICENSE) © 2026 Hussenet Thomas and contributors.
