# packrat-cli

[![check](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml/badge.svg)](https://github.com/packratlabs/packrat-cli/actions/workflows/check.yml)

The Packrat CLI lets you work with [Packrat](https://packrat.sh) from your terminal. With it, you can:

- See how a pull request performed in production, with the analyst's verdict for each deploy.
- Schedule checks that the analyst runs later, such as "the nightly job ran without errors".
- Read Packrat pages, including reports linked from Packrat emails.
- Query your traces and logs with SQL.
- Scan a Rails project to see which observability tools it uses, and how to send each signal to
  Packrat.

## Install

Run the install script:

```
curl -fsSL https://packrat.sh/cli | sh
```

It downloads the newest release for your platform, verifies it against the release's
`checksums.txt`, and puts `packrat` in `~/.local/bin`. To install somewhere else, set
`PACKRAT_INSTALL_DIR`. `packrat` is a single binary with no dependencies.

Each release includes an archive for each platform below, and a `checksums.txt` file with the
SHA-256 checksum of each archive:

| Platform | Archive |
| --- | --- |
| macOS, Apple silicon | `packrat_darwin_arm64.tar.gz` |
| macOS, Intel | `packrat_darwin_amd64.tar.gz` |
| Linux, x86-64 | `packrat_linux_amd64.tar.gz` |
| Linux, arm64 | `packrat_linux_arm64.tar.gz` |
| Windows, x86-64 | `packrat_windows_amd64.zip` |

To install a release by hand, download the archive for your platform, verify it against
`checksums.txt`, extract it, and move `packrat` to a directory on your `PATH`.

To build from source instead, use Go 1.26: `make build` writes `bin/packrat`.

### Update

```
packrat update
```

`packrat update` downloads the newest release, verifies it against `checksums.txt`, and replaces
the `packrat` you ran with it, wherever it's installed.

When a newer release is out, a command that asks Packrat something ends with a notice on stderr, at
most once a day:

```
packrat 0.2.0 is available (you have 0.1.0). Run: packrat update
```

To turn the notice off, set `PACKRAT_NO_UPDATE_NOTICE=1`. If Packrat stops accepting your version,
commands fail with `packrat 0.1.0 is too old for this Packrat. Run packrat update.`

## Authenticate

Run `packrat login` to sign in with your browser:

```
$ packrat login
Opened https://packrat.sh/cli/activate in your browser.
Confirm that it shows the code BCDF-GHJK, then sign in.
Waiting...
Signed in to Acme as dana@example.com.
```

Signing in creates an API key for you on this machine. On macOS, the key is stored in your login
Keychain. On other platforms, it's stored in `~/.config/packrat/credentials.json` with mode `0600`.
On macOS, `credentials.json` holds only the org and your email address.

The key appears with your name on your org's API keys page, where you or anyone in the org can
revoke it. It stops working if you leave the org. If you sign in to a second org, commands use that
org. Run `packrat auth status` to see which org and key are in use.

To sign out and revoke the key, run `packrat logout`.

In CI, set `PACKRAT_API_KEY` instead. Commands use `PACKRAT_API_KEY` if it's set, and the key from
`packrat login` otherwise. `packrat logout` doesn't affect `PACKRAT_API_KEY`.

## Commands

Run `packrat help` for a list of commands, and `packrat help COMMAND` for a command's usage.

### pr show

`packrat pr show` shows how a pull request performed in production:

```
$ packrat pr show https://github.com/acme/shop/pull/482
=== #482 Cache the cart total
shop/production

Deploy    9f2c41e8, live 2026-10-14 16:02 UTC
Verdict   worked in part
Report    packrat open https://query.packrat.sh/reports/rep_5k2m9x0qv7ha

Asked for
  A faster cart page.

Evidence
  p95 went from 840 ms to 210 ms. 37 checkouts with a gift card show the wrong total.

Next
  Key the cache on gift cards.

=== Checks
STATUS               WHAT                                                     ID
due 20 Oct 21:00     Gift card checkouts charge the total the page shows.     chk_7ha5k2m9x0qv

Times are UTC. packrat check show <ID> prints the whole text.
```

By default, the app is the one whose analyst is connected to the pull request's repository. For a
URL, that's the repository in the URL. For a number, it's the `origin` remote of the current
directory. To use a different app, pass `--app` or set `PACKRAT_APP`. Pass `--json` to output the API
response.

### check

A check asks Packrat's analyst to verify something at a later time. Write what must be true and the
SQL that shows it in a file, then pass it on stdin:

```
packrat check add --pull 482 --due 2026-10-20T21:00Z < check.txt
```

The analyst runs the check after the `--due` time and emails the result in the thread of the pull
request's verdict email. With `--pull` and no `--app`, the app is the one connected to the `origin`
remote.

For a check that isn't tied to a pull request, pass `--report` instead. The result is emailed in the
thread of that analyst report:

```
packrat check add --due 2026-10-20T21:00Z --report rep_5k2m9x0qv7ha < check.txt
```

To manage checks:

- `packrat check list` lists the app's checks. Pass `--pull 482` to list only that pull request's checks.
- `packrat check show chk_…` shows a check's full text, and its result once it has run.
- `packrat check close chk_…` cancels a check before it runs.

### open

`packrat open URL` prints a Packrat page: as markdown when the page has a markdown version, and as
HTML otherwise. You can pass a whole pasted line, such as the last line of a Packrat email, and
`packrat open` reads the first URL in it. A GitHub pull request URL runs `packrat pr show`.

`packrat open` only sends your key to `https://packrat.sh` and its subdomains, and to
`PACKRAT_URL` and `PACKRAT_QUERY_URL` if they're set.

### sql

`packrat sql` runs a SQL query against an app:

```
packrat sql "select count(*) from packrat_spans(since := current_date)"
```

You can also pass the query with `-f FILE`, or on stdin. Rows are printed to stdout as NDJSON, and a
summary line with the app that answered is printed to stderr. Pass `--quiet` to skip the summary
line. For the tables and macros you can query, see the [API reference](https://packrat.sh/docs/api).

### apps

`packrat apps` lists the apps that have sent data to Packrat:

```
$ packrat apps
APP               LAST DATA
shop/production   now
shop/development  3 hours ago
```

If you don't recognize an app, it's usually one of yours reporting under a different name, such as
an unset service name or an unexpected environment.

### status

`packrat status` shows what Packrat receives from an app: each signal, the sources that send it, and
whether Packrat keeps it.

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

- `--app` names the app, as `SERVICE/ENV` from `packrat apps`. You can leave it out if your org has
  one app.
- `no` in the KEPT column means a source sends the signal, but Packrat doesn't store it, because the
  app already keeps that signal from another source. Logs, metrics and all browser data are kept
  from every source.
- Times are accurate to the minute. Anything under two minutes is shown as "now".
- `--wait` checks every 3 seconds until new data arrives, for up to 5 minutes. Use it during setup:
  run the command, then start your app and load a page. Without `--app`, it waits for any app to
  send data, including one Packrat hasn't seen before, and then shows that app.
- `--json` outputs the API response, described in the [API reference](https://packrat.sh/docs/api#get-status).

`packrat status` exits with:

- `0` if data arrived in the last hour.
- `1` if no data arrived in the last hour, a kept source stopped while another source still sends
  the same signal, `--wait` timed out, or the API rejected the request.
- `2` for a usage error or a missing key.

### Network requests

Commands that query Packrat send only your API key, in the `Authorization` header, the app name, and
the CLI's version, in the `User-Agent` header. They never print your key. `packrat update` asks
GitHub for the newest release and sends no key. To use a different API host, set `PACKRAT_QUERY_URL`. To use a different
site host (for `login` and `open`), set `PACKRAT_URL`.

## Set up a project

`packrat setup detect` and `packrat setup plan` scan a project and suggest how to send its data to
Packrat. Ruby on Rails is the only supported stack.

`detect` reports what the project uses now: the stack, observability gems and their configuration,
agents and collectors, browser SDKs, and the names of environment variables. `plan` uses those
results to choose one route for each signal. Neither command changes anything. The steps in a plan
describe what a future `packrat setup` command will do.

### setup detect

```
$ packrat setup detect
packrat setup detect  shop  Rails 8.1.1, Ruby 3.3.8

  SIGNAL      FOUND
  Exceptions  Sentry
  Traces      OpenTelemetry
  Logs        none
  Replays     Sentry browser
```

FOUND lists each vendor in the project that sends the signal in production, including vendors that
Packrat can't receive from, such as New Relic. `none` means nothing sends the signal.

- `PATH` is the directory to scan. It defaults to the current directory.
- `--verbose` (`-v`) shows everything that was found: each vendor, the file and line that configures
  it, its gems, agents and browser SDKs, and the names of environment variables.
- `--json` outputs the full results as a JSON document.
- `--host` also checks the machine running the command, and shows the results under "This machine,
  not production". It's off by default, because that machine is usually a laptop.

`packrat setup detect` exits with `0` when the scan completes, `1` when the path can't be read, and
`2` for a usage error.

### setup plan

```
$ packrat setup plan
packrat setup plan  shop  Rails 8.1.1, Ruby 3.3.8

  SIGNAL      SOURCE          INSTALLED  STEP
  Exceptions  Sentry          ✓          dual-ship to Packrat
  Traces      OpenTelemetry   ✓          dual-ship to Packrat
  Logs        OpenTelemetry              add the OpenTelemetry logs gems
  Replays     Sentry browser  ✓          dual-ship to Packrat

Add --app service/env to detect if Packrat is receiving data from each signal.
```

- SOURCE is where Packrat will receive the signal from. The names match the ones `packrat status`
  shows.
- INSTALLED has a check mark if the project already includes that source.
- STEP is what setup would do. "Dual-ship" means the source keeps sending to its current vendor and
  also sends to Packrat.
- A note below the table explains when the usual route wasn't chosen, or why a signal has no route.
- `--verbose` (`-v`) shows the reason for each step. `--json` outputs the plan as JSON.
- `plan` takes the same `PATH` as `detect`, but has no `--host` option.

A project's files can't show where a source sends its data, because that address is usually set in
a production environment variable, and `plan` never reads variable values. So a project that already
sends to Packrat gets the same steps. To skip signals that Packrat already receives, pass
`--app SERVICE/ENV`. `plan` then checks with Packrat, as `packrat status` does:

```
$ packrat setup plan --app colorbook/production
packrat setup plan  colorbook  Rails 8.0.2, Ruby 3.3.6

  SIGNAL      SOURCE                         INSTALLED  STEP
  Exceptions  OpenTelemetry                  ✓          none: Packrat receives it
  Traces      OpenTelemetry, Sentry browser  ✓          none: Packrat receives it
  Logs        OpenTelemetry                  ✓          none: Packrat receives it
  Replays     Sentry browser                 ✓          none: Packrat receives it

Nothing to do. Packrat already receives 4 signals.
```

With `--app`, SOURCE is the source that sends the signal now. This can differ from what the project
files suggest: an app keeps its exceptions and traces from the first source that sent them, so
Packrat wouldn't keep data from a second source.

`packrat setup plan` exits with `0` when it produces a plan, `1` when the project isn't a supported
stack or the path can't be read, and `2` for a usage error.

## What detect reads

`detect` is read-only. It doesn't write files or make network requests, and it never reads the value
of an environment variable from a file. Without `--host`, it opens no sockets. The test
`internal/detect/nonetwork_test.go` enforces this.

With `--host`, it also checks three things on the machine:

- whether `/etc/datadog-agent/datadog.yaml` or `/opt/datadog-agent` exists
  (`C:\ProgramData\Datadog\datadog.yaml` on Windows);
- whether something accepts a TCP connection on `localhost:8126`, the port of the Datadog trace
  Agent. It opens the connection and closes it without sending any data. This is the only socket
  `detect` ever opens;
- whether `DD_AGENT_HOST` or `OTEL_EXPORTER_OTLP_ENDPOINT` is set in the environment. Only the name
  is recorded.

It reads these files:

- `Gemfile.lock`, `Gemfile` and `.ruby-version`, for the stack and the gems.
- `config/application.rb`, `config/environments/*.rb`, `config/initializers/*.rb`, and the Ruby
  files under `lib` and `app/lib`, for the line that configures each vendor.
- `Procfile`, `Dockerfile*`, `docker-compose*.yml`, `compose*.yml`, `config/deploy.yml`, `app.json`,
  `fly.toml`, `heroku.yml`, `.buildpacks`, `.github/workflows/*.yml`, `bin/*`, `datadog/*`, and everything under
  `k8s`, `kubernetes`, `deploy`, `helm` and `charts`, for an agent or a collector.
- `config/importmap.rb`, `package.json`, `app/javascript/**` and `app/views/**`, for browser SDKs.
- `.env`, `.env.*`, `.env_*`, `.env-*` and the deploy files above, for the **names** of environment variables.

It never reads or prints:

- The value of an environment variable. `detect` keeps the text before the first `=` or `:` and
  ignores the rest. It keeps a name only if it begins with `DD_`, `DATADOG_`,
  `SENTRY_`, `NEW_RELIC_`, `OTEL_`, `HONEYBADGER_`, `BUGSNAG_`, `ROLLBAR_`, `SCOUT_`, `APPSIGNAL_`,
  `ELASTIC_APM_`, `POSTHOG_` or `PACKRAT_`.
- A line of an env file or a deploy file. Evidence from these files is only the path and the line
  number.
- A symlink, a file over 2 MB, or anything under `.git`, `node_modules`, `vendor/bundle`, `tmp`,
  `log`, `storage`, `public/assets` and `public/packs`.

The test `internal/detect/canary_test.go` enforces this. It puts a marker value in each of these files
in `testdata/canary`, and fails if the marker appears in the output.

## The JSON

### The detect document

`packrat setup detect --json` outputs one document with the schema `packrat.detect/1`. Removing or
renaming a key, or changing what a value means, changes the schema name. Two runs on the same files
produce identical output. Lists are never `null`; an empty list is `[]`.

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

`packrat setup plan --json` outputs one document with the schema `packrat.plan/1`.

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

`make release-dry` runs [GoReleaser](https://goreleaser.com) with `.goreleaser.yaml` in snapshot
mode. It builds the five release binaries, their archives and `checksums.txt` in `dist/`, without
publishing anything or creating a tag. The first run compiles GoReleaser, which takes a few minutes.

Each directory in `testdata/` is a small repository that tests one behavior. The expected `detect` and
`plan` output for each is in `internal/detect/testdata/golden/`. To regenerate those files, run
`UPDATE=1 go test ./internal/detect -run Golden`, and review the diff before you commit it.

## The corpus

The detection rules are tuned against nine real Rails repositories. `corpus/repos.txt` lists them, each
pinned to a commit. `corpus/expected/` holds the expected `detect` and `plan` JSON for each, and
`corpus/NOTES.md` describes what each repository taught us.

```
make corpus                                       # clone the public ones into corpus/src (2 GB)
go test ./internal/detect -run Corpus -v           # compare each with its expected file
UPDATE=1 go test ./internal/detect -run Corpus     # rewrite the expected files
```

`make corpus` is the only command here that uses the network. The test skips any repository that
isn't cloned, so it does nothing in CI.

Go 1.26, standard library only.
