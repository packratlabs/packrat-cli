# packrat-cli

[![check](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml/badge.svg)](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml)

`packrat audit` reads a repository and says what observability it has, and which route gets each
signal (exceptions, logs, traces, replays) into [Packrat](https://packrat.sh). Ruby on Rails is the
only stack yet.

The audit is not complete. Today it finds the stack and the observability gems. It does not yet
find configuration, agents, browser SDKs or environment variable names, and it does not pick routes.

## Usage

```
packrat audit [path] [--json] [--host]
```

- `path` is the directory to audit. The default is the current directory.
- `--json` prints the JSON document instead of the table.
- `--host` will also probe the machine that runs the audit. It has no effect yet.

Exit status: `0` when the audit ran, `1` when the path cannot be read, `2` for a usage error.

## What is read, and what is never read

The audit is read-only. It makes no network call and writes no file. It never reads the value of an
environment variable from any file.

Files read today: `Gemfile.lock`, `Gemfile` and `.ruby-version`. A file over 2 MB or a symlink is
not read.

## The JSON

The schema is `packrat.audit/1`. Its keys will be documented here when the audit fills them.

## Development

```
make build    # bin/packrat
make check    # gofmt, vet, staticcheck, golangci-lint, tests
```

Each directory in `testdata/` is a small repository for one behaviour. The output the audit must
give for it is in `internal/audit/testdata/golden/`. `UPDATE=1 go test ./internal/audit -run Golden`
rewrites those files; read the diff before you commit it.

Go 1.26, standard library only.
