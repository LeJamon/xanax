# Getting started

## Install

With Go 1.26.5 or newer:

```sh
go install github.com/LeJamon/rvr/cmd/rvr@latest
```

Tagged releases publish checksummed macOS and Linux archives for amd64 and arm64 on [GitHub Releases](https://github.com/LeJamon/rvr/releases).

To build a checkout instead:

```sh
go build -o rvr ./cmd/rvr
```

## Start the dashboard

```sh
rvr
```

Pass a repository path to scope the dashboard and new sessions to that location:

```sh
rvr ~/code/api
```

## Launch an agent

```sh
rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"
printf '%s\n' "long prompt" | rvr new -
```

The dashboard prompt box can also launch several background sessions. Press `Enter` to launch, or `Ctrl+O` to launch and attach immediately.
