# Dashboard

The session list and prompt box form one navigable column. A path-scoped dashboard shows only the sessions beneath that repository and launches new work there.

## Prompt box

- `Enter` launches a session in the background.
- `Ctrl+O` launches and attaches to the session.
- `Tab` opens the harness picker for the next session.
- `Esc` clears a draft prompt.

## Session list

- `Enter` or `→` opens a live session or stored logs.
- `Space` toggles a live output peek.
- `l` shows stored logs.
- `e` renames the rvr-only label.
- `r` resumes a finished or interrupted session.
- `Ctrl+X` removes a session; live sessions require a second confirmation.
- `/` filters sessions.

## Attach and detach

Opening a session enters the harness’s native TUI. Every harness key is forwarded while attached. Press `Ctrl+Q` by default to detach; the session keeps running in the background.

Sessions survive dashboard exits and reboots. On the next launch, rvr can auto-resume interrupted sessions with the harness’s native resume behavior.
