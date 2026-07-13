# Command reference

```sh
rvr                                    # dashboard: all sessions
rvr ~/code/api                         # dashboard scoped to one path
rvr new [flags] [prompt ...]           # create a session
rvr list [--json]                      # aliases: ls, ps
rvr attach <id>                        # attach to a live session
rvr resume <id>                        # reattach or relaunch natively
rvr kill <id>                          # terminate, retain the record
rvr rm <id>... [--force]               # remove sessions
rvr prune                              # remove terminal sessions
rvr logs <id> [-f]                     # print or follow raw output
rvr config                             # print resolved configuration
```

Session IDs accept unique prefixes, like Git commit IDs.

## New session flags

Use `--harness` to select a configured harness and `--repo` to choose the working repository. A prompt may be passed as arguments or through standard input.

```sh
rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"
```
