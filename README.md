# xanax

A terminal-first session manager for autonomous AI coding agents — `tmux + htop`
for harnesses like **opencode** and **pi**. xanax launches agents, supervises them
so they keep running after you close the UI, and gives you one dashboard to attach,
monitor, and manage them regardless of which harness is doing the work.

xanax is **not** an agent framework: no planning, prompting, memory, or tools. That
stays the harness's job. See [`SPEC.md`](SPEC.md) for the full v1 design.

## Status

MVP core. Works end-to-end with any PTY CLI via the generic adapter; native
adapters for opencode and pi are implemented against their current APIs (see
[`tasks/todo.md`](tasks/todo.md) for what's verified vs. pending live testing).

## Install

Requires Go 1.24+.

```sh
go build -o xanax ./cmd/xanax
```

Runs on macOS and Linux.

## Usage

```sh
xanax                                    # dashboard: all sessions
xanax ~/code/api                         # dashboard scoped to one path
xanax new --harness opencode "fix the failing tests"
xanax new --harness pi --repo ~/code/api "add pagination"
xanax list [--json]
xanax attach <id>                        # reattach to a live session
xanax resume <id>                        # reattach, or relaunch a dead one natively
xanax kill   <id>
xanax logs   <id> [-f]                   # print (or follow) a session's raw output
xanax config                             # print resolved config + paths
```

Given a path, the dashboard shows only sessions under it and launches new ones there.

Session IDs accept unique prefixes (git-style).

### Navigation

The sessions and the prompt box form one column. `↑`/`↓` always move the selection;
the selected row is framed with accent-colored top/bottom rules.

- **Prompt box selected** (bottom row): type/paste a prompt and press **Enter** to
  launch a new session in the background (fire off several), or **Ctrl+O** to launch
  **and attach** — you land in the harness's own input with its native `/commands` and
  `@file` syntax (empty prompt = fresh harness). **Tab** opens the harness picker to
  choose which harness the next session uses. You can only type when it's selected.
- **A session selected:** `→`/`Enter` open its window · `e` rename · `r` resume ·
  `k` remove · `/` filter · `↓` back to the prompt box · `Ctrl+C` quit. (Rename is a
  xanax-only label; it never touches the harness's own session.) A live peek of the
  selected session's screen shows in a pane above the prompt, and each row shows its
  live git branch (and open PR number, via `gh`) on the right.
- **Session window:** opening a session drops you into the harness's own live TUI (the
  conversation). Press **Left arrow** (or `ctrl+\`) to detach — the session keeps
  running in the background; **Right arrow** from the list opens it. (Left/right are
  intercepted by xanax as back/into, so they aren't sent to the harness.)

Sessions survive closing the dashboard and even a reboot: on the next launch, xanax
auto-resumes interrupted sessions via the harness's native resume (like Claude
Code's background agents).

## Configuration

Optional, at `~/.config/xanax/config.toml`. opencode and pi work with no config.

```toml
default_harness   = "opencode"
auto_resume       = true
notifications     = true
interact_exit_key = "ctrl+\\"

[harness.opencode]
adapter = "opencode"
command = "opencode"

[harness.pi]
adapter = "pi"
command = "pi"

# Any other PTY CLI works via the generic adapter:
[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]
resume_args = ["session", "--resume"]
```

## Layout

```
cmd/xanax            entrypoint
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
go test ./...
go vet ./...
```
