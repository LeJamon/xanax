# Xanax — v1 Specification

Terminal-first session manager for autonomous AI coding agents. One place to launch,
monitor, attach to, and manage agent sessions regardless of which harness runs them.

Xanax is **not** an agent framework: no planning, no prompting, no memory, no tools.
It is a control plane over existing harnesses (tmux + htop for AI agents).

- Language: **Go** (pure-Go dependencies preferred, single static binary)
- Platforms: macOS (arm64/x86_64), Linux (glibc)
- v1 harnesses: **opencode**, **pi**, and **codex** (architecture stays
  harness-generic; codex uses the generic adapter)

---

## 1. Decisions (confirmed 2026-07-02)

| # | Decision | Resolution |
|---|----------|------------|
| D1 | State detection | Native side channels per harness (opencode SSE, pi extension); harnesses without a channel get running/exited only |
| D2 | Supervision model | One detached supervisor **process per session** |
| D3 | Resume semantics | Reattach while the supervisor is alive. Interrupted sessions (reboot, supervisor crash) are **auto-resumed on the next xanax launch** via the harness's native resume flag and the captured session ref (§6). `xanax resume` covers manual cases. |
| D4 | v1 extras | Core loop only; desktop notifications next in line |

Rationale for D1/D2 in §5 and §4. Deferred features in §12.

---

## 2. Research findings that constrain the design

Verified against docs and source on 2026-07-02. Both projects move fast — **pin the
harness versions we test against** and degrade gracefully on mismatch.

### opencode (moved: `sst/opencode` → `anomalyco/opencode`; TUI rewritten in TS)
- The TUI is client+server. Launching with `--port <n>` (default host 127.0.0.1)
  exposes an HTTP API on the live TUI process.
- `GET /event` is an SSE stream: `session.status` (`idle` | `busy` | `retry`),
  `session.idle`, `permission.asked`/`permission.replied`, `session.error`. This is a
  reliable running/waiting signal. `GET /session/status` exists as a polling fallback
  (dev branch — verify against the release we pin).
- Prompt injection into the TUI: `POST /tui/append-prompt` then `POST /tui/submit-prompt`.
- Sessions stored under `~/.local/share/opencode/storage/`; resume with
  `opencode --session <id>` (`-c` for most recent). Session IDs appear in SSE events.
- **Escape is bound to "interrupt agent"** in the TUI. Leader key is `ctrl+x`.
- Exit codes are best-effort (history of exit-0-on-error bugs). Don't trust them alone.
- `opencode run` (headless) auto-rejects permission prompts unless `--auto` — not
  suitable for interactive supervision; we use the TUI + API instead.

### pi (moved: `badlogic/pi-mono` → `earendil-works/pi`; binary `pi`)
- Interactive TUI takes the initial prompt as a positional arg: `pi "fix tests"`.
- Extensions (TypeScript, loaded via `-e file.ts` or `~/.pi/agent/extensions/`)
  receive `agent_start`, `agent_end`, `session_start`, `session_shutdown`,
  `tool_execution_*` events and can run arbitrary code — a tiny xanax hook extension
  is the reliable state channel for TUI mode.
- Headless `--mode rpc` exists (JSON commands in, JSONL events out, `get_state`)
  — not needed for v1 but confirms the event vocabulary.
- Sessions are JSONL trees under `~/.pi/agent/sessions/--<cwd-with-dashes>--/`;
  resume with `pi --session <path|id>`, `-c` for most recent in cwd.
- **Escape is bound to "interrupt agent"**; most ctrl-keys are taken by emacs-style
  editing bindings. `ctrl+\` appears free in both harnesses.
- Useful env: `PI_SKIP_VERSION_CHECK=1`, `PI_CODING_AGENT_DIR`, settings
  `quietStartup`. Exit codes undocumented.

**Consequences:**
1. The original spec's "Escape returns to dashboard" is impossible — Escape interrupts
   the agent in both TUIs. Xanax uses **arrow-key navigation** for its own chrome and
   reserves passthrough-exit for a key both harnesses leave free, **`ctrl+\`** (§10).
2. Terminal-output regex scraping would fight full-screen TUI escape sequences;
   both harnesses offer better channels — hence D1.
3. `completed` vs `failed` cannot rest on exit codes; the state channel and
   who-initiated-termination feed the decision (§6).

---

## 3. Process model

Three process roles, one binary:

```
xanax (CLI / dashboard TUI)          — foreground, short- or long-lived
  └── xanax _supervise <session-id>  — one detached process per session (hidden subcommand)
        └── harness process           — opencode / pi inside a PTY
```

- `xanax new` inserts the session row, then spawns `xanax _supervise <id>` detached
  (setsid, stdio → per-session log file) and returns. Optionally attaches immediately
  (`--attach`, default true when stdout is a TTY).
- The **supervisor** owns: the PTY, an in-memory output ring buffer, the raw output
  log, the state side channel, a unix control socket, and all SQLite writes for its
  session. It outlives the UI; sessions keep running when the dashboard exits.
- The **dashboard/CLI** reads SQLite for the session list and connects to each live
  session's socket for streaming (attach) and live state updates.
- Crash isolation: one supervisor dying (or a xanax binary upgrade) never affects
  other sessions. Liveness = "can I connect to its socket"; stale rows (pid gone,
  socket dead) are reconciled to `failed` with reason `orphaned` on discovery.

SQLite is opened in WAL mode by all processes; the supervisor is the only writer for
its session's rows, so contention is trivial.

## 4. Supervisor internals

```
PTY master ──► ring buffer (256 KiB) ──► attached clients (0..n)
     ▲               └──► raw log file (logs/<id>.raw, 10 MiB cap, truncate-rotate)
     │
  stdin bytes from attached client
State channel (adapter) ──► state machine ──► SQLite (sessions.status, events)
                                          └─► pushed to connected clients
Unix socket server ──► attach / input / resize / subscribe / kill / info
```

- **PTY**: `creack/pty`. Size follows the most recent attacher; on attach with an
  unchanged size, jiggle rows by −1/+1 to force SIGWINCH (harmless nudge for apps
  that do repaint on it).
- **Server-side terminal emulator (vt10x).** The supervisor feeds all harness output
  through an embedded VT emulator that maintains the true screen state. Modern TUIs
  (opencode's opentui) render *differentially* against their own internal frame
  buffer and never repaint cells they believe are already on screen — so an attaching
  client cannot rely on the app to redraw. Instead:
  - **Full-screen harness (alt-screen tracked via `\x1b[?1049h`/`l`):** attach replays
    an exact **snapshot** rendered from the emulator — reset+clear, every cell painted
    with its colors/attributes (truecolor preserved), cursor restored. The client's
    display then matches the app's internal buffer, and subsequent diffs apply
    cleanly. This is the tmux model.
  - **Line-based harness:** attach replays the scrollback ring (history matters more).
  - The client's initial resize frame is applied **before** the snapshot is rendered,
    so the snapshot matches the client's dimensions (a wrong-width snapshot wraps
    every row and scrolls the content away).
- **Sequence-safe chunking.** PTY reads can split an escape sequence or UTF-8 rune
  across chunks; a client attaching between such chunks would start mid-sequence and
  print the tail literally (";2;255;255;255m" garbage). The supervisor normalizes all
  broadcast boundaries with a small VT parser (`splitSafe`): incomplete trailing
  sequences are carried into the next chunk (bounded at 8 KiB for malformed output).
- **Socket protocol**: newline-delimited JSON control frames + length-prefixed binary
  frames for PTY I/O. Messages: `hello`, `snapshot` (ring buffer replay), `output`,
  `input`, `resize`, `state`, `detach`, `kill`, `info`. Multiple simultaneous clients
  allowed (dashboard subscribes for state while a terminal is attached).
- **Sockets** live in `/tmp/xanax-<uid>/<session-id>.sock` (kept short: macOS
  `sun_path` limit is 104 bytes).
- **Termination**: `kill` message → SIGTERM to the harness process group, SIGKILL
  after 5 s. Supervisor records exit code, final state, `ended_at`, then exits.
- **Terminal fidelity (why harnesses don't crash).** The harness runs in a bare PTY,
  which is *not* a terminal emulator, so two things are handled explicitly:
  - **Stable `TERM`.** The harness is launched with `TERM=xterm-256color` and
    `COLORTERM=truecolor`, overriding whatever xanax inherited. Without this a harness
    launched from Ghostty (`TERM=xterm-ghostty`) emits Ghostty-only sequences that
    break when rendered in, or attached from, any other terminal — the "crashes unless
    Ghostty" failure.
  - **Query answering.** TUIs emit capability queries at startup (DSR cursor
    position/size, primary/secondary device attributes, background/foreground color,
    Kitty-keyboard, synchronized-output) and block or crash if unanswered. Because the
    harness starts before any client attaches, the supervisor answers these itself
    while no client is connected; once a client attaches, its real terminal answers and
    the supervisor stays quiet (no double replies).
- **Detach triggers.** The attach client detaches on `ctrl+\` (configurable
  `interact_exit_key`) **or the Left arrow** — Left returns you to the dashboard and is
  therefore not forwarded to the harness. Both triggers are recognized in every
  encoding a harness may negotiate through the passthrough. The Left arrow: legacy
  `ESC[D`, application cursor `ESCOD`, and the Kitty keyboard protocol's parameterized
  forms (`ESC[1D`, `ESC[1;1D`) that harnesses like codex push (`CSI > u`). The exit key:
  the raw control byte and, under the Kitty disambiguate flag, its `CSI u` form (`ctrl+\`
  → `ESC[92;5u`). Only an *unmodified key press* detaches: `Ctrl`/`Alt`/`Shift`+Left pass
  through for word navigation and selection, and repeat/release events are ignored so a
  modifier released just before Left (reported as an unmodified Left release) does not
  eject you mid-edit.

## 5. Adapter architecture

The adapter contract is generic; opencode and pi get native implementations (D1).
A `generic` adapter runs any CLI from config with running/exited states only, so new
harnesses work day one and gain rich states only when someone writes an adapter.

```go
type Adapter interface {
    // Command to exec for a new or resumed session (argv, env, cwd).
    LaunchSpec(s *Session, resume bool) (LaunchSpec, error)
    // Deliver the initial prompt once the harness is up (PTY write or API call).
    SendInitialPrompt(ctx context.Context, s *Session, pty io.Writer) error
    // Stream normalized state events until ctx is done. May return ErrNoChannel.
    WatchState(ctx context.Context, s *Session) (<-chan StateEvent, error)
    // Harness-native session reference for later resume ("" if unknown yet).
    SessionRef(ctx context.Context, s *Session) (string, error)
}

type StateEvent struct {
    Kind    StateKind // AgentBusy, AgentIdle, NeedsInput, HarnessError
    Message string    // optional human-readable detail (e.g. permission question)
    At      time.Time
}
```

### opencode adapter
- Allocate a free localhost port; launch `opencode --hostname 127.0.0.1 --port <n>`
  with cwd = repo. Initial prompt via the TUI's `--prompt <text>` flag; resume with
  `--session <ref>` instead.
- **Do not set `OPENCODE_SERVER_PASSWORD`.** Setting it makes opencode's *own* TUI
  client fail auth against its own server (401) and crash on startup — confirmed in
  testing. opencode manages auth for its in-process client itself; xanax stays out of
  it and watches the event stream unauthenticated.
- `WatchState`: SSE client on `GET /event` (envelope `{id,type,properties}`);
  `session.status` busy/retry → AgentBusy, idle → AgentIdle,
  `session.idle` → AgentIdle, `permission.updated` → NeedsInput (title as detail),
  `session.error` → HarnessError. If the server returns 401/403 (auth required),
  xanax **stops watching** — the harness still works, the session just degrades to
  running/exited with no fine-grained state.
- `SessionRef`: first sessionID observed on the SSE stream.
- Note: opencode's current `dev` branch uses `permission.updated` (not
  `permission.asked`); verify against the pinned release.

### pi adapter
- Launch `pi -e <hook.mjs> "<prompt>"` with cwd = repo, `PI_SKIP_VERSION_CHECK=1`,
  `XANAX_HOOK_SOCKET=<sock>`.
- `hook.mjs` is embedded in the xanax binary (`go:embed`) and materialized under the
  data dir. It is an ESM **default-export factory** (`export default function(pi)`)
  — pi loads extensions through jiti. It opens `XANAX_HOOK_SOCKET` (`node:net`) and
  reports `agent_start`/`agent_end`/`session_start`/`session_shutdown` as JSON lines.
  The session file path (for resume) is read from
  `ctx.sessionManager.getSessionFile()` inside the handlers, not from the payload.
- The hook socket listener is created before launch (the hook connects at startup).
- `WatchState`: `agent_start` → AgentBusy, `agent_end` → AgentIdle.
- `SessionRef`: session JSONL path reported by the hook.
- Resume: `pi -e <hook.mjs> --session <ref>`.

### generic adapter (config-only)
Runs any PTY CLI from TOML — the day-one path for a new harness before anyone
writes a native adapter. `command`, `args`, `resume_args`, `env` from config.
- **Prompt delivery:** `prompt_arg = "--flag"` passes the initial prompt as a flag
  value, `prompt_positional = true` appends it as the last argument, else it is typed
  into the PTY (a fallback that races full-screen TUIs). This is what makes the
  composer/`xanax new` flow reliable for a generic harness.
- **Resume:** with `resume_args` set to the harness's "continue last session in this
  repo" flag (e.g. `-c`), a generic session is resumable — both `xanax resume` and
  auto-resume-after-reboot treat a configured `resume_args` as "resumable" even
  though generic captures no session ref. Precise when there is one session per repo.
- **State inference (approximate, opt-in):** with no native state channel the
  supervisor can still infer needs-input from the output stream — `idle_timeout = N`
  marks the session *waiting* after N seconds of silence; `waiting_pattern = "regexp"`
  marks it *waiting* on an output match (e.g. `\(y/n\)`). Either resets to *running*
  when output resumes. Best-effort; a native adapter is authoritative.
- **Add from the dashboard:** the harness picker's `+` opens a form (name / command /
  prompt arg / idle timeout / waiting pattern) that appends a generic `[harness.name]`
  block to `config.toml`, reloads, and selects it — so a new harness, including its
  state-inference signals, is registered without editing files by hand. A multi-word
  command is split into `command` + `args`.

## 6. Session state model

```
starting ──► running ⇄ waiting          (AgentBusy / AgentIdle+NeedsInput)
                │
                ▼ (harness process exits)
   completed | failed | cancelled
```

- `waiting` = agent idle and expecting user input (opencode `idle`/`permission.asked`,
  pi `agent_end`). For an interactive TUI this includes "task done, prompt ready".
- Terminal states, decided at process exit:
  - `cancelled` — xanax initiated the kill.
  - `completed` — exit code 0 **and** last known agent state was idle.
  - `failed` — everything else (non-zero exit, exit while busy, unresumable orphan).
- Every transition is appended to `events` (immutable log) and mirrored to
  `sessions.status`.

### Interrupted sessions & auto-resume

A session whose supervisor is gone (reboot, crash, SIGKILL) while its status was
`starting`/`running`/`waiting` is **interrupted**, not failed. A reconciliation pass
runs on dashboard startup and before `list`/`attach`/`resume`:

1. For each session in a live state, probe its socket.
2. Dead socket, `auto_resume = true` (default), and `harness_session_ref` present →
   respawn `xanax _supervise` in resume mode; the session reappears, typically as
   `waiting` (same behavior as Claude Code's persistent background agents).
3. Dead socket and no session ref (harness never reported one) →
   `failed` with detail `orphaned`.

Native resume only reopens the harness session idle at its prompt — it does **not**
re-trigger agent work — so auto-resume is safe and spends no tokens.

## 7. Storage

| What | Where |
|------|-------|
| Config | `~/.config/xanax/config.toml` |
| Database | `~/.local/share/xanax/xanax.db` (SQLite, WAL, `modernc.org/sqlite`) |
| Raw session output | `~/.local/share/xanax/logs/<session-id>.raw` |
| Supervisor logs | `~/.local/share/xanax/logs/<session-id>.supervisor.log` |
| pi hook extension | `~/.local/share/xanax/pi/hook.ts` (re-materialized per version) |
| Sockets | `/tmp/xanax-<uid>/<session-id>.sock` |

XDG paths are used on both macOS and Linux (`XDG_CONFIG_HOME`/`XDG_DATA_HOME`
respected) — consistent with how opencode and pi behave.

### Schema (v1)

```sql
CREATE TABLE sessions (
  id                 TEXT PRIMARY KEY,   -- UUIDv4
  title              TEXT NOT NULL,      -- user-supplied or derived from prompt
  repo_path          TEXT NOT NULL,
  branch             TEXT,               -- recorded at launch, informational
  harness            TEXT NOT NULL,      -- config key: "opencode" | "pi" | ...
  harness_session_ref TEXT,              -- native resume handle
  initial_prompt     TEXT,
  status             TEXT NOT NULL,      -- starting|running|waiting|completed|failed|cancelled
  status_detail      TEXT,               -- e.g. permission question, failure reason
  pid                INTEGER,            -- supervisor pid
  socket_path        TEXT,
  exit_code          INTEGER,
  created_at         TEXT NOT NULL,      -- RFC3339 UTC
  updated_at         TEXT NOT NULL,
  ended_at           TEXT
);
CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  ts         TEXT NOT NULL,
  type       TEXT NOT NULL,              -- created|state|prompt_sent|exited|killed|error
  payload    TEXT                        -- JSON
);
CREATE TABLE repositories (
  path         TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  last_used_at TEXT NOT NULL
);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- settings holds schema_version; migrations are forward-only numbered scripts.
```

## 8. Configuration

```toml
# ~/.config/xanax/config.toml
default_harness   = "opencode"
auto_resume       = true         # revive interrupted sessions on launch (§6)
notifications     = true         # desktop notifications on needs-input/completed/failed
interact_exit_key = "ctrl+\\"    # step out of raw passthrough back to navigate mode (§10);
                                 # arrows/Escape are reserved by the harness TUIs

# TUI colors — ANSI palette index ("0"–"255") or hex ("#rgb"/"#rrggbb"); omitted
# fields keep their defaults. Full set: accent, waiting, running, completed,
# failed, cancelled, muted, text, branch, pr.
[theme]
accent = "13"
muted  = "250"
branch = "6"

# Dashboard key bindings (§10). Each action takes a list of keys; any of them
# fires it, and an omitted action keeps its default. Keys use bubbletea names —
# a rune ("o", "/", "+"), a named key ("enter", "up", "tab", "shift+tab"), a
# control chord ("ctrl+c", "ctrl+o"), or "space". An empty list unbinds an
# action. `xanax config` prints the full resolved set. `confirm`/`cancel` are the
# shared primary/back keys reused across the composer, picker, filter, rename and
# harness form; the interact-mode detach key is separate (interact_exit_key).
# Precedence: `quit` is matched before every mode, and `up`/`down` before the
# session list and composer, so if you bind one of them to a key a context action
# also uses (e.g. up = ["up", "k"] while remove keeps "k"), the global one wins.
[keys]
remove = ["k", "ctrl+k"]         # override just the actions you want to remap
filter = ["ctrl+f", "/"]
# up, down, confirm, cancel, quit, open, resume, rename, preview, quit_list,
# launch_attach, harness_picker, add_harness, set_default, modify_harness,
# toggle_search, form_next, form_prev — all remappable the same way.

[harness.opencode]
adapter = "opencode"             # native adapter
command = "opencode"             # override for custom install paths

[harness.pi]
adapter = "pi"
command = "pi"

# codex is a built-in generic full-screen TUI harness. If a config overrides it,
# omitted fields keep these defaults.
[harness.codex]
adapter           = "generic"
command           = "codex"
prompt_positional = true                  # codex "<prompt>" starts a session with it
resume_args       = ["resume", "--last"]  # reattach to the most recent session
idle_timeout      = 120                   # no native state; mark "waiting" when idle

# Any additional harness works immediately with basic states:
[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]                # start goose's interactive session
resume_args = ["session", "--resume"]    # resume the most recent session
# A CLI that accepts the prompt as a flag can add prompt_arg = "--flag"
# (or prompt_positional = true) to deliver it on the command line instead.

```

Defaults for opencode, pi, and codex are built in; the file is optional.

## 9. CLI

```
xanax                       # dashboard (default command)
xanax new [flags] "prompt"  # --harness, --repo (default "."), --title, --attach/--no-attach
xanax list [--json]
xanax attach <id-or-prefix>
xanax resume <id-or-prefix> # reattach if alive, else native-resume relaunch (D3)
xanax kill   <id-or-prefix>
xanax config                # print resolved config + paths
xanax _supervise <id>       # hidden; internal supervisor entrypoint
```

Session IDs accept unique prefixes everywhere (git-style).

## 10. Dashboard & attach UX

Two layers, both driven by the arrow keys. `←` always means "back / up one level."

### Management view (dashboard)

- Bubble Tea full-screen app. Sessions grouped by state, most actionable first:
  **Needs input** → **Running** → **Completed** → **Cancelled** → **Failed**.
- Each row: status glyph, title, harness, repo name, relative age, and (for waiting
  sessions) the status detail.
The sessions and the **prompt box** form one navigable column; the prompt box is the
last row. `↑`/`↓` always move the selection. The selected row is framed with full-width
top and bottom rules (no left/right sides) in the navigation accent color; the prompt
box shows the same rules in grey when it is not the selected row.

- **Prompt box selected:** typed/pasted text goes into it; **Enter** launches a new
  session in the selected harness (current repo, no attach, so you can fire off
  several). **Ctrl+O** launches **and attaches** — landing directly in
  the harness's own input, where its native syntax (`/commands`, `@file` completion,
  palettes) is fully available; with an empty prompt it opens a fresh harness. (**Alt+Enter**
  is a best-effort alias that works only on terminals reporting it as a single key; Ctrl+O
  is the portable binding.) xanax
  deliberately does not re-implement harness syntax in its own composer (prompts are
  delivered verbatim; interactive syntax belongs to the harness). **Tab** opens the
  harness picker: a list of all configured harnesses (`↑`/`↓` + Enter, Esc cancels) —
  scales to any number of harnesses. The composer label always names the harness the
  next session will use. `↑` moves up into the sessions.
- **A session selected:** you are not typing, so plain letters act on it — `→`/`Enter`
  open the live window, `k` remove (terminate if live, then delete from the list),
  `r` resume, `e` **rename** (a xanax-only UI label; never touches the harness's own
  session), `s` **settings** (open the keybindings editor, below), `↓` returns to the
  prompt box. `Ctrl+K`/`Ctrl+R` are aliases for terminals that deliver them; `Ctrl+C`
  always quits.
- **Rename** opens an inline single-line editor pre-filled with the current title;
  Enter saves to the `sessions.title` column, Esc cancels.
- Every dashboard key above is a default, not a hard-coded binding: the `[keys]`
  table in the config (§8) remaps any of them, and the footer hints follow suit.
  Only the interact-mode detach key lives elsewhere (`interact_exit_key`), since
  it is read inside the raw passthrough rather than the dashboard. `quit` is
  resolved before any mode and `up`/`down` before the session list and composer,
  so a key bound to one of those wins over a context action bound to the same key.
- **Keybindings editor** (`s`): a centered, searchable modal — the same style as
  the harness picker — listing every action with its current keys. `↑`/`↓` move and
  Enter starts a capture for the highlighted action; then press one or more keys
  (each added once) and Enter again to save them — so you can bind aliases like `k`
  and `ctrl+k` together — or Esc to cancel. The binding is written to the `[keys]`
  table and takes effect immediately; Esc in the list closes the editor. It edits
  the same file `[keys]` documents, so changes made here and by hand are one and the
  same. Enter, Esc and the quit key drive the capture itself, so binding an action
  to one of those is a config-file edit.
- Live updates: the list reloads from SQLite once per second (supervisors are the
  writers). The startup reconciliation pass auto-resumes interrupted sessions (§6)
  before the first render.

### Session window (interact)

Opening a session enters a raw, byte-for-byte passthrough to the agent's own
full-screen TUI — the "conversation window". This is where you *see and drive*
opencode/pi; xanax supervises and proxies the PTY but never interprets the screen.
On entry it replays the ring buffer and forces a SIGWINCH repaint, then proxies
transparently. Every key — arrows, Escape, the agent's editing and leader bindings —
reaches the harness unmodified, so the agent stays fully usable. Because the
passthrough is total, stepping back out needs a key neither harness binds:
**`ctrl+\`** (configurable `interact_exit_key`) detaches.

The dashboard enters interact mode by shelling out to `xanax attach <id>` via Bubble
Tea's process exec, so the attach client owns the whole terminal and hands it back
cleanly on `ctrl+\` — no input reader leaks back into the dashboard. If the session's
supervisor is gone but a harness session ref was captured, opening it resumes
natively instead.

`xanax attach <id>` from a plain shell is the same client, standalone; `ctrl+\`
detaches and exits the process.

This keeps xanax's chrome (composer + list) from ever stealing keys from the agent
while the user is actually driving it: keys only reach xanax's UI when you're *not*
in a session window.

## 11. Libraries

| Concern | Choice |
|---------|--------|
| CLI | `spf13/cobra` |
| TUI | `charmbracelet/bubbletea` + `lipgloss` + `bubbles` |
| PTY | `creack/pty` |
| VT emulator | `hinshun/vt10x` (screen state for attach snapshots) |
| SQLite | `modernc.org/sqlite` (pure Go, CGO-free) |
| Config | `BurntSushi/toml` |
| Logging | `log/slog` (JSON to supervisor log files) |
| IDs | `google/uuid` |

## 12. Explicitly deferred (post-v1)

- Desktop notifications on needs-input/completed (first in line; trivial once state
  events exist — osascript / notify-send).
- Dashboard search & filters; multi-repo/workspace grouping.
- Cost & token tracking (opencode API exposes usage; pi via session stats/JSONL).
- Pause/resume via SIGSTOP (in-flight API calls time out while stopped — needs care).
- Branch/worktree creation, session templates, queue manager, cross-provider
  migration, web dashboard, Windows.

## 13. Build order

1. **Scaffold** — module, cobra skeleton, config loading, SQLite + migrations.
2. **Supervisor core** — PTY spawn, ring buffer, socket protocol, detached launch;
   prove with `$SHELL` as a fake harness: new → detach → attach → kill survives
   dashboard exit.
3. **Adapters** — generic first, then pi (hook extension), then opencode (SSE).
4. **Dashboard** — list, grouping, live state, attach/detach integration.
5. **Resume & reconciliation** — native resume, auto-resume of interrupted sessions
   on launch, orphan detection, terminal states.
6. **Tests** — config parsing, migrations, adapter LaunchSpec construction, state
   machine transitions, socket protocol round-trip; manual end-to-end on macOS+Linux.
