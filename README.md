# packrat-cli

[![check](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml/badge.svg)](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml)

`packrat audit` reads a repository and says what observability it has, and which route gets each
signal (exceptions, logs, traces, replays) into [Packrat](https://packrat.sh). Ruby on Rails is the
only stack yet.

It finds the stack, the observability gems and their configuration, agents and collectors, browser
SDKs, and the names of environment variables. From these it picks one route for each signal. It does
not set anything up; the routes say what a later `packrat setup` would do.

## Usage

```
packrat audit [path] [--verbose] [--json] [--host]
```

```
packrat audit  shop  Rails 8.1.1, Ruby 3.3.8

  Exceptions  ready        through Sentry, which you already use
  Traces      ready        through OpenTelemetry, which you already use
  Logs        needs setup  add the OpenTelemetry logs gems
  Replays     ready        through the Sentry browser SDK, which you already use

3 of 4 signals are ready.
Add --verbose to see what was found and why.

Nothing was changed. Nothing was sent.
```

"ready" means that Packrat can receive what the repository already sends, with no new library.

- `path` is the directory to audit. The default is the current directory.
- `--verbose`, or `-v`, adds the route and the reason under each line, and then everything that was
  found: each vendor, the file and line that configures it, its gems, agents, browser SDKs and the
  names of environment variables.
- `--json` prints the JSON document instead of the table. It always holds everything.
- `--host` also probes the machine that runs the audit, and prints the result under "This machine,
  not production". It is off by default, because that machine is usually a laptop.

Exit status: `0` when the audit ran, `1` when the path cannot be read, `2` for a usage error.

## What is read, and what is never read

The audit is read-only. It writes no file and sends nothing. It never reads the value of an
environment variable from any file. Without `--host` it opens no socket; the test
`internal/audit/nonetwork_test.go` enforces this.

With `--host` it also looks at three things on the machine:

- whether `/etc/datadog-agent/datadog.yaml` or `/opt/datadog-agent` exists
  (`C:\ProgramData\Datadog\datadog.yaml` on Windows);
- whether something accepts a TCP connection on `localhost:8126`, the port of the Datadog trace
  Agent. The audit opens the connection and closes it. It sends no byte. This is the only socket
  the audit ever opens;
- whether `DD_AGENT_HOST` or `OTEL_EXPORTER_OTLP_ENDPOINT` has a value in the environment. Only
  the name is kept.

What it reads:

- `Gemfile.lock`, `Gemfile` and `.ruby-version`, for the stack and the gems.
- `config/application.rb`, `config/environments/*.rb`, `config/initializers/*.rb`, and the Ruby
  files under `lib` and `app/lib`, for the line that configures each vendor.
- `Procfile`, `Dockerfile*`, `docker-compose*.yml`, `compose*.yml`, `config/deploy.yml`, `app.json`,
  `fly.toml`, `heroku.yml`, `.buildpacks`, `.github/workflows/*.yml`, `bin/*`, `datadog/*`, and everything under
  `k8s`, `kubernetes`, `deploy`, `helm` and `charts`, for an agent or a collector.
- `config/importmap.rb`, `package.json`, `app/javascript/**` and `app/views/**`, for browser SDKs.
- `.env`, `.env.*`, `.env_*`, `.env-*` and the deploy files above, for the **names** of environment variables.

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

`--json` prints one document. The schema is `packrat.audit/1`. A change that removes or renames a
key, or changes what a value means, will change the schema name. Two runs on the same files print
the same bytes. A list is never `null`; an empty list is `[]`.

```json
{
  "schema": "packrat.audit/1",
  "path": "/Users/x/shop",
  "stack": { "name": "rails", "version": "8.1.1", "ruby": "3.3.8", "evidence": [] },
  "sdks": [],
  "agents": [],
  "browser": [],
  "env_names": [],
  "signals": [],
  "host": null,
  "notes": []
}
```

| Key | Meaning |
|---|---|
| `schema` | Always `packrat.audit/1`. |
| `path` | The absolute path that was audited. |
| `stack` | The framework, or `null` when the repository is not a supported stack. `name` is `rails`. `version` and `ruby` are empty strings when they are not known. |
| `sdks` | One entry for each observability gem. Sorted by `vendor`, then `gem`. |
| `agents` | One entry for each agent or collector that the repository starts or configures. Sorted by `vendor`. |
| `browser` | One entry for each browser SDK. Sorted by `vendor`. |
| `env_names` | Names of environment variables, sorted by `name`. Never a value. |
| `signals` | The route for each signal. Empty when `stack` is `null`; otherwise always four, in the order `exceptions`, `traces`, `logs`, `replays`. |
| `host` | `null` without `--host`. |
| `notes` | Sentences about the audit as a whole, such as a missing `Gemfile.lock`. |

**Evidence.** Every fact has `evidence`: a list of `{ "path", "line", "text" }`, sorted by `path`,
then `line`. `path` is relative to the audited directory and uses `/` on every operating system.
`line` starts at 1; it is `0` when the evidence is that the file exists. `text` is the trimmed line,
cut to 200 bytes. It is empty for an env file or a deploy file, and for an agent it is only the
pattern that matched.

**An entry of `sdks`.**

| Key | Meaning |
|---|---|
| `vendor` | For example `Sentry`, `Datadog`, `OpenTelemetry`. Logging gems have the vendor `logging`. |
| `gem`, `version` | The gem and its locked version. `version` is empty when there is no `Gemfile.lock`. |
| `direct` | `true` when the gem is in the `DEPENDENCIES` of the lockfile. `false` for a dependency of another gem. |
| `groups` | The Gemfile groups of the gem. Empty means every group. A gem with only `development` and `test` does not load in production and picks no route. |
| `configured` | `true` when a config file configures the vendor. |
| `provides` | What the gem can send: `exceptions`, `traces`, `logs`, `logs shape`, `product analytics`. |
| `notes` | Short facts, such as `traces off` or `gem present, no configure call found`. |

**An entry of `agents`** has `vendor` and `evidence`.

**An entry of `browser`** has `vendor`, `package` (empty when the SDK is loaded by a script tag),
`replays` (`true` when session replay is on) and `evidence`.

**An entry of `env_names`** has `name` and `files`, the sorted paths that name it.

**An entry of `signals`.**

| Key | Meaning |
|---|---|
| `name` | `exceptions`, `traces`, `logs` or `replays`. |
| `route` | How Packrat would take the signal. One of the routes below. |
| `state` | `route`: setup can add this route. `already`: the repository already sends this way, and setup only points it at Packrat. `later`: not offered yet; no rule gives this state today. `unsupported`: there is no route. |
| `reason` | One sentence that says why. |
| `blocked` | Routes that would normally be chosen, each with the reason why not. |

| Signal | Routes |
|---|---|
| `exceptions` | `Sentry copy`, `Datadog Agent copy`, `OpenTelemetry exceptions (Rails.error)` |
| `traces` | `Datadog Agent copy`, `OpenTelemetry`, `OpenTelemetry (Rails)` |
| `logs` | `Datadog Agent copy`, `OpenTelemetry logs` |
| `replays` | `Sentry browser copy`, `Sentry browser SDK pointed at Packrat`, `none` |

**`host`**, with `--host`.

| Key | Meaning |
|---|---|
| `agent_paths` | The Datadog Agent paths that exist on this machine. |
| `trace_listener` | `true` when something accepts a connection on `localhost:8126`. |
| `env_set` | Which of `DD_AGENT_HOST` and `OTEL_EXPORTER_OTLP_ENDPOINT` have a value. Names only. |
| `notes` | Problems during the probe. |

## Development

```
make build    # bin/packrat
make check    # gofmt, vet, staticcheck, golangci-lint, tests
```

Each directory in `testdata/` is a small repository for one behaviour. The output the audit must
give for it is in `internal/audit/testdata/golden/`. `UPDATE=1 go test ./internal/audit -run Golden`
rewrites those files; read the diff before you commit it.

## The corpus

The rules are tuned against nine real Rails repositories. `corpus/repos.txt` lists them with a
pinned commit. `corpus/expected/` holds the JSON that the audit must give for each.
`corpus/NOTES.md` says what each repository taught.

```
make corpus                                       # clone the public ones into corpus/src (2 GB)
go test ./internal/audit -run Corpus -v           # compare each with its expected file
UPDATE=1 go test ./internal/audit -run Corpus     # rewrite the expected files
```

`make corpus` is the only command here that uses the network. The test skips a repository that is
not on the machine, so it does nothing in CI.

Go 1.26, standard library only.
