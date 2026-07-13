# Configuration

Configuration is optional. opencode, pi, and codex work with no file. To override defaults or add a generic harness, create `~/.config/rvr/config.toml`.

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
```

## Codex

Codex uses the generic full-screen adapter by default. Override it only when needed:

```toml
[harness.codex]
adapter           = "generic"
command           = "codex"
full_screen       = true
prompt_positional = true
resume_args       = ["resume", "--last"]
idle_timeout      = 120
```

## Generic harness example

```toml
[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]
resume_args = ["session", "--resume"]
```

Run `rvr config` to inspect the full resolved configuration, paths, and defaults.
