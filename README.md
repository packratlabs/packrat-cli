# packrat-cli

[![check](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml/badge.svg)](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml)

`packrat audit` reads a repository and says what observability it has, and which route gets each
signal (exceptions, logs, traces, replays) into [Packrat](https://packrat.sh). Ruby on Rails is the
only stack yet.

The audit is not complete. Today it finds the stack, the observability gems and their
configuration, agents and collectors, browser SDKs, and the names of environment variables. It does
not pick routes yet.

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

What it reads:

- `Gemfile.lock`, `Gemfile` and `.ruby-version`, for the stack and the gems.
- `config/application.rb`, `config/environments/*.rb` and `config/initializers/*.rb`, for the line
  that configures each vendor.
- `Procfile`, `Dockerfile*`, `docker-compose*.yml`, `compose*.yml`, `config/deploy.yml`, `app.json`,
  `fly.toml`, `heroku.yml`, `.buildpacks`, `.github/workflows/*.yml`, `bin/*`, and everything under
  `k8s`, `kubernetes`, `deploy`, `helm` and `charts`, for an agent or a collector.
- `config/importmap.rb`, `package.json`, `app/javascript/**` and `app/views/**`, for browser SDKs.
- `.env`, `.env.*` and the deploy files above, for the **names** of environment variables.

What it never reads or prints:

- The value of an environment variable. The audit keeps what is left of the first `=` or `:` and
  does not look at the rest. It keeps a name only when the name begins with `DD_`, `DATADOG_`,
  `SENTRY_`, `NEW_RELIC_`, `OTEL_`, `HONEYBADGER_`, `BUGSNAG_`, `ROLLBAR_`, `SCOUT_`, `APPSIGNAL_`,
  `ELASTIC_APM_`, `POSTHOG_` or `PACKRAT_`.
- A line of an env file or a deploy file. Evidence from these files is the path and the line
  number, with no text.
- A symlink, a file over 2 MB, or anything under `.git`, `node_modules`, `vendor/bundle`, `tmp`,
  `log`, `storage`, `public/assets` and `public/packs`.

The test `internal/audit/canary_test.go` enforces this. It plants a marker value in every such file
of `testdata/canary` and fails if the marker reaches the output.

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
