# Harnesses

rvr is harness-agnostic. It handles session supervision and observation; adapters translate harness-specific state and resume behavior into that shared layer.

| Harness | Integration | State detection | Resume behavior |
| --- | --- | --- | --- |
| opencode | Native adapter, local SSE API | Busy, idle, permission/input, error | Captured session ID |
| pi | Native adapter, embedded hook | Agent busy/idle lifecycle | Captured session file |
| codex | Generic full-screen adapter | Output pattern plus idle timeout | `codex resume --last` |
| Other PTY CLIs | Configured generic adapter | Optional waiting pattern and idle timeout | Configured `resume_args` |

If a native side channel is unavailable or changes upstream, the harness keeps running and rvr degrades to process-level running/exited state.

## Generic adapters

Any compatible PTY CLI can use the generic adapter. Provide the command, start arguments, and resume arguments; set `prompt_arg` or `prompt_positional` if the CLI accepts the prompt directly. For diff-rendered TUIs, set `full_screen = true`.
