# packrat-cli

[![check](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml/badge.svg)](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml)

`packrat` reads a repository, and asks Packrat what production did.

- `packrat login` signs this machine in to one of your orgs, in your browser. `packrat logout` and
  `packrat auth status` go with it.
- `packrat pr` says how a pull request went in production: the analyst's verdict on each deploy that
  shipped it, the evidence, and its watches. `packrat pr … watch` asks the analyst to verify
  something for it later, and `packrat watch` lists or closes watches.
- `packrat open` reads a Packrat URL: a page, a report, or the URL in a line from a Packrat mail.
- `packrat sql` runs SQL against an app.
- `packrat detect` reads a repository and says what observability it has now. Ruby on Rails is the
  only stack yet.
- `packrat plan` says how each signal (exceptions, traces, logs, replays) gets from that repository
  into [Packrat](https://packrat.sh).
- `packrat apps` lists the apps that Packrat has data from.
- `packrat status` says what Packrat receives for one app: each signal, each source that sends it,
  and whether Packrat keeps it.

`detect` gives facts and `plan` gives steps. `detect` finds the stack, the observability gems and
their configuration, agents and collectors, browser SDKs, and the names of environment variables.
`plan` picks one route for each signal from these facts. It does not set anything up; its steps say
what a later `packrat setup` would do.

## Install

There is no release yet. Until the first one, build from this repository with Go 1.26:

```
git clone git@github.com:packratlabs/packrat-cli.git
cd packrat-cli
make build              # writes bin/packrat
bin/packrat detect ~/projects/my-app
```

`packrat` is one file with no dependencies. Copy `bin/packrat` to a directory on your `PATH`, such
as `/usr/local/bin`, to run it from anywhere.

After the first release, each release will have one archive for each of these, and a
`checksums.txt` with the SHA-256 of each archive:

| System | Archive |
| --- | --- |
| macOS, Apple silicon | `packrat_darwin_arm64.tar.gz` |
| macOS, Intel | `packrat_darwin_amd64.tar.gz` |
| Linux, x86-64 | `packrat_linux_amd64.tar.gz` |
| Linux, arm64 | `packrat_linux_arm64.tar.gz` |
| Windows, x86-64 | `packrat_windows_amd64.zip` |

Download the archive for your system, compare it with `checksums.txt`, unpack it, and put
`packrat` on your `PATH`.

### For an agent

```
curl -fsSL https://packrat.sh/cli | sh
```

This script does not exist yet. The line is here so that the install command is decided before the
script is written. Do not run it until this note is removed.

## Usage

```
packrat detect [path] [--verbose] [--json] [--host]
packrat plan [path] [--verbose] [--json]
packrat apps [--json]
packrat status --app service/env [--wait] [--json]
```

### detect

```
packrat detect  shop  Rails 8.1.1, Ruby 3.3.8

  SIGNAL      FOUND
  Exceptions  Sentry
  Traces      OpenTelemetry
  Logs        none
  Replays     Sentry browser
```

FOUND names each vendor in the repository that sends the signal in production. It includes a vendor
that Packrat cannot receive from, such as New Relic. `none` means that nothing sends the signal.

- `path` is the directory to read. The default is the current directory.
- `--verbose`, or `-v`, adds everything that was found: each vendor, the file and line that configures it, its gems, agents, browser SDKs and the
  names of environment variables.
- `--json` prints the JSON document instead of the table. It always holds everything.
- `--host` also probes the machine that runs the detection, and prints the result under "This machine,
  not production". It is off by default, because that machine is usually a laptop.

Exit status: `0` when the detection ran, `1` when the path cannot be read, `2` for a usage error.

### plan

```
packrat plan  shop  Rails 8.1.1, Ruby 3.3.8

  SIGNAL      SOURCE          INSTALLED  STEP
  Exceptions  Sentry          ✓          dual-ship to Packrat
  Traces      OpenTelemetry   ✓          dual-ship to Packrat
  Logs        OpenTelemetry              add the OpenTelemetry logs gems
  Replays     Sentry browser  ✓          dual-ship to Packrat

Add --app service/env to detect if Packrat is receiving data from each signal.
```

- SOURCE is what Packrat will receive the signal from. The names are the names that
  `packrat status` prints.
- INSTALLED has a check mark when the repository has that source now.
- STEP is what setup would do. "dual-ship" means that the source continues to send to its vendor
  and also sends to Packrat.
- A line under the table says why a usual route was not chosen, or why a signal has no route.
- `--verbose`, or `-v`, adds the reason under each step. `--json` prints the plan document.
- `plan` takes the same `path` as `detect`. It has no `--host`.

The repository cannot show where a source sends: that address is usually a variable of the
production environment, and `plan` never reads the value of a variable. So a repository that
already ships to Packrat gets the same steps. `--app service/env` asks Packrat what it receives for
that app, as `packrat status` does, and a signal that Packrat already keeps has no step:

```
$ packrat plan --app colorbook/production
packrat plan  colorbook  Rails 8.0.2, Ruby 3.3.6

  SIGNAL      SOURCE                         INSTALLED  STEP
  Exceptions  OpenTelemetry                  ✓          none: Packrat receives it
  Traces      OpenTelemetry, Sentry browser  ✓          none: Packrat receives it
  Logs        OpenTelemetry                  ✓          none: Packrat receives it
  Replays     Sentry browser                 ✓          none: Packrat receives it

Nothing to do. Packrat already receives 4 signals.
```

With `--app`, SOURCE is the source that sends the signal now. It can be different from the source
that the repository alone suggests: an app keeps its exceptions and its traces from the first source
that sent them, so a second source would not be kept.

Exit status: `0` when there is a plan, `1` when the repository is not a supported stack or the path
cannot be read, `2` for a usage error.

### The key

Every command that asks Packrat uses `PACKRAT_API_KEY` when it is set, as in CI, and otherwise the
key of `packrat login`.

```
$ packrat login
Your browser opened https://packrat.sh/cli/activate.
Check that it shows the code BCDF-GHJK, choose the org, and sign in.
Waiting...
Signed in to Acme as dana@example.com.
```

The key that `login` makes is yours, for this machine. The org's keys page lists it with your name,
and you or anyone in the org can revoke it there. It stops when you leave the org. On macOS it is
kept in the login Keychain; elsewhere in `~/.config/packrat/credentials.json` (mode `0600`).
`credentials.json` keeps the org and your address, and on macOS nothing more. Sign in to a second org
and that one becomes the one used; `packrat auth status` says which.

`packrat logout` revokes the key and forgets it. It does not touch `PACKRAT_API_KEY`.

### pr, watch, open and sql

```
$ packrat pr https://github.com/acme/shop/pull/482
packrat pr  #482  shop/production

Deploy 9f2c41e8, live 2026-10-14 16:02 UTC: worked in part  Cache the cart total
  Asked for: A faster cart page.
  Evidence: p95 went from 840 ms to 210 ms. 37 checkouts with a gift card show the wrong total.
  Next: Key the cache on gift cards.
  Report: packrat open https://query.packrat.sh/reports/rep_5k2m9x0qv7ha

Watches
  wch_7ha5k2m9x0qv #482 due 2026-10-20 21:00 UTC
    Gift card checkouts charge the total the page shows.
```

- The app is `--app` (or `PACKRAT_APP`), else the app whose analyst is connected to the pull
  request's repository: the one in the URL, or the `origin` remote of the current checkout for a
  number. `--json` prints the answer of the API.
- `packrat pr 482 watch --due 2026-10-20T21:00Z < watch.txt` asks the analyst to verify something at
  that time. The text says what must be true and the SQL that shows it. The answer is mailed in the
  thread of the pull request's verdict.
- `packrat watch list` lists the app's watches, `--pull 482` only that pull request's.
  `packrat watch close wch_…` closes one before it is verified.
- `packrat open <URL>` prints what a Packrat URL shows, as markdown where the page has it and as HTML
  where it does not. Give it a whole pasted line: it reads the first URL in it. The key is sent only to
  `https://packrat.sh` and its subdomains, and to `PACKRAT_URL` and `PACKRAT_QUERY_URL` when they are
  set. A GitHub pull request URL is the same as `packrat pr`.
- `packrat sql "select …"` (or `-f file`, or stdin) prints NDJSON rows, and a line on stderr that
  names the app that answered. `--quiet` leaves that line out.

### apps and status

These two commands ask `https://query.packrat.sh` with your key.

```
$ packrat apps
APP               LAST DATA
shop/production   now
shop/development  3 hours ago
```

An app that you do not know is usually your app with a different name: a service name that was not
set, or an environment that you did not expect.

```
$ packrat status --app colorbook/production
packrat status  colorbook/production

  SIGNAL      SOURCE           KEPT  LAST DATA
  Exceptions  Sentry           yes   2 minutes ago
              OpenTelemetry    no    2 minutes ago
  Traces      Sentry           yes   now
              Datadog          no    3 minutes ago
              Sentry browser   yes   6 minutes ago
  Logs        Datadog          yes   4 minutes ago
              OpenTelemetry    yes   now
  Metrics     nothing yet
  Replays     Sentry browser   yes   6 minutes ago

  Packrat keeps the first source of exceptions and of traces.
  To use a different source, ask us to switch it.

4 of 5 signals are arriving.
```

- `--app` is necessary. It is `service/env`, as `packrat apps` prints it.
- `no` under KEPT means that the source sends the signal, and Packrat does not store it, because the app
  keeps that signal from a different source. Logs, metrics and all data from a browser are kept from
  each source.
- A time is exact to the minute. A time of less than two minutes is "now".
- `--wait` asks again each 3 seconds until new data arrives, for 5 minutes at most. Use it during
  setup: start the command, then start the app and load a page.
- `--json` prints the answer of the API. The [API page](https://packrat.sh/docs/api#get-status)
  describes it.

Exit status of `status`: `0` when data arrived in the last hour; `1` when nothing arrived in the
last hour, when a kept source stopped while a different source sends the same signal, when `--wait`
got no new data, or when the API refused the request; `2` for a usage error or no key.

What these commands send: the API key, as the `Authorization` header, and the name of the app.
They send nothing else, and never print the key. `PACKRAT_QUERY_URL` replaces the address of the
API, and `PACKRAT_URL` the address of the site, where `login` and pages are.

## What detect reads, and what it never reads

`detect` is read-only. It writes no file and sends nothing. It never reads the value of an
environment variable from any file. Without `--host` it opens no socket; the test
`internal/detect/nonetwork_test.go` enforces this.

With `--host` it also looks at three things on the machine:

- whether `/etc/datadog-agent/datadog.yaml` or `/opt/datadog-agent` exists
  (`C:\ProgramData\Datadog\datadog.yaml` on Windows);
- whether something accepts a TCP connection on `localhost:8126`, the port of the Datadog trace
  Agent. The detection opens the connection and closes it. It sends no byte. This is the only socket
  the detection ever opens;
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

- The value of an environment variable. The detection keeps what is left of the first `=` or `:` and
  does not look at the rest. It keeps a name only when the name begins with `DD_`, `DATADOG_`,
  `SENTRY_`, `NEW_RELIC_`, `OTEL_`, `HONEYBADGER_`, `BUGSNAG_`, `ROLLBAR_`, `SCOUT_`, `APPSIGNAL_`,
  `ELASTIC_APM_`, `POSTHOG_` or `PACKRAT_`.
- A line of an env file or a deploy file. Evidence from these files is the path and the line
  number, with no text.
- A symlink, a file over 2 MB, or anything under `.git`, `node_modules`, `vendor/bundle`, `tmp`,
  `log`, `storage`, `public/assets` and `public/packs`.

The test `internal/detect/canary_test.go` enforces this. It plants a marker value in every such file
of `testdata/canary` and fails if the marker reaches the output.

## The JSON

### The detect document

`--json` prints one document. The schema is `packrat.detect/1`. A change that removes or renames a
key, or changes what a value means, will change the schema name. Two runs on the same files print
the same bytes. A list is never `null`; an empty list is `[]`.

```json
{
  "schema": "packrat.detect/1",
  "path": "/Users/x/shop",
  "stack": { "name": "rails", "version": "8.1.1", "ruby": "3.3.8", "evidence": [] },
  "sdks": [],
  "agents": [],
  "browser": [],
  "env_names": [],
  "host": null,
  "notes": []
}
```

| Key | Meaning |
|---|---|
| `schema` | Always `packrat.detect/1`. |
| `path` | The absolute path that was read. |
| `stack` | The framework, or `null` when the repository is not a supported stack. `name` is `rails`. `version` and `ruby` are empty strings when they are not known. |
| `sdks` | One entry for each observability gem. Sorted by `vendor`, then `gem`. |
| `agents` | One entry for each agent or collector that the repository starts or configures. Sorted by `vendor`. |
| `browser` | One entry for each browser SDK. Sorted by `vendor`. |
| `env_names` | Names of environment variables, sorted by `name`. Never a value. |
| `host` | `null` without `--host`. |
| `notes` | Sentences about the detection as a whole, such as a missing `Gemfile.lock`. |

**Evidence.** Every fact has `evidence`: a list of `{ "path", "line", "text" }`, sorted by `path`,
then `line`. `path` is relative to the directory that was read and uses `/` on every operating system.
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

**`host`**, with `--host`.

| Key | Meaning |
|---|---|
| `agent_paths` | The Datadog Agent paths that exist on this machine. |
| `trace_listener` | `true` when something accepts a connection on `localhost:8126`. |
| `env_set` | Which of `DD_AGENT_HOST` and `OTEL_EXPORTER_OTLP_ENDPOINT` have a value. Names only. |
| `notes` | Problems during the probe. |

### The plan document

`packrat plan --json` prints one document. The schema is `packrat.plan/1`.

```json
{
  "schema": "packrat.plan/1",
  "path": "/Users/x/shop",
  "app": null,
  "stack": { "name": "rails", "version": "8.1.1", "ruby": "3.3.8", "evidence": [] },
  "steps": [
    { "name": "exceptions", "source": "Sentry", "installed": true, "step": "dual-ship to Packrat",
      "route": "Sentry copy", "state": "route",
      "reason": "sentry-ruby sends; Packrat takes the same envelope", "blocked": [], "arrives": false }
  ],
  "notes": []
}
```

`steps` always has four entries, in the order `exceptions`, `traces`, `logs`, `replays`.

| Key | Meaning |
|---|---|
| `name` | `exceptions`, `traces`, `logs` or `replays`. |
| `source` | What Packrat will receive the signal from: `Sentry`, `Datadog`, `OpenTelemetry`, `Sentry browser`, or `none`. |
| `installed` | `true` when the repository has that source now. |
| `step` | What setup would do, as the table prints it. |
| `route` | The rule's name for the way in. One of the routes below. |
| `state` | `route`: setup can add this route. `already`: the repository already sends this way, and setup only points it at Packrat. `later`: not offered yet; no rule gives this state today. `unsupported`: there is no route. |
| `reason` | One sentence that says why. |
| `blocked` | Routes that would normally be chosen, each with the reason why not. |
| `arrives` | `true` when Packrat already keeps the signal for the app of `--app`. Always `false` without `--app`. |

`app` is the app of `--app`, or `null`.

| Signal | Routes |
|---|---|
| `exceptions` | `Sentry copy`, `Datadog Agent copy`, `OpenTelemetry exceptions (Rails.error)` |
| `traces` | `Datadog Agent copy`, `OpenTelemetry`, `OpenTelemetry (Rails)` |
| `logs` | `Datadog Agent copy`, `OpenTelemetry logs` |
| `replays` | `Sentry browser copy`, `Sentry browser SDK pointed at Packrat`, `none` |

## Development

```
make build    # bin/packrat
make check    # gofmt, vet, staticcheck, golangci-lint, tests
```

`make release-dry` runs [GoReleaser](https://goreleaser.com) from `.goreleaser.yaml` in snapshot
mode. It builds the five release binaries, their archives and `checksums.txt` into `dist/`. It
publishes nothing and makes no tag. The first run compiles GoReleaser and takes some minutes.

Each directory in `testdata/` is a small repository for one behaviour. The output that `detect` and
`plan` must give for it is in `internal/detect/testdata/golden/`. `UPDATE=1 go test ./internal/detect -run Golden`
rewrites those files; read the diff before you commit it.

## The corpus

The rules are tuned against nine real Rails repositories. `corpus/repos.txt` lists them with a
pinned commit. `corpus/expected/` holds the two JSON documents that `detect` and `plan` must give for each.
`corpus/NOTES.md` says what each repository taught.

```
make corpus                                       # clone the public ones into corpus/src (2 GB)
go test ./internal/detect -run Corpus -v           # compare each with its expected file
UPDATE=1 go test ./internal/detect -run Corpus     # rewrite the expected files
```

`make corpus` is the only command here that uses the network. The test skips a repository that is
not on the machine, so it does nothing in CI.

Go 1.26, standard library only.
