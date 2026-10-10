# What the corpus taught

Each repository was read by hand before its first run: the `Gemfile`, the `Gemfile.lock`, the
initializers, the deploy files and the browser code. The expectation below was written from that
reading. "Changed" is the rule that was fixed because the tool and the repository disagreed. Each
fix has a fixture in `testdata/` named for it.

The public repositories are pinned in `repos.txt`. Packrat and Colorbook are local directories with
no pin, so their expected files go out of date when those repositories change. Run
`UPDATE=1 go test ./internal/audit -run Corpus` and read the diff.

## The lessons

1. **A common word is not a pattern.** `.use ` matched `config.middleware.use Rack::Attack`, and
   `Sentry.init` matched `Sentry.initialized?`. A config call must now be a whole word, and a
   common word counts only in a file that configures the vendor.
2. **Configuration is not always in `config/`.** Two of nine repositories call
   `OpenTelemetry::SDK.configure` from `lib` or `app/lib`. Both are now read.
3. **Not every Rails application depends on `rails`.** Discourse names `railties` and the other
   parts one by one.
4. **A gem that is present is not a gem that is in use.** Chatwoot locks six vendors, each with
   `require: false`, each started only when an environment variable is set. The audit cannot know
   which one production uses. The route rules take the Datadog gem as the answer for traces, and
   for Chatwoot that is a guess. This is not fixed; see "Open".
5. **The plan's beliefs about other people's repositories were half wrong.** Docuseal has no Sentry
   and no Rollbar. Forem has no Honeycomb gem. OpenProject has AppSignal but Airbrake, not Sentry.
   Reading the lockfile first is what caught these, not the tool.

## chatwoot

Expected: Rails 7.2.3.1, Ruby 3.4.4. Datadog, Elastic APM, New Relic, Scout and Sentry gems, all
`require: false`. Datadog and Sentry are configured in initializers behind `if ENV[...]`; the other
three have a file in `config/`. Sentry's trace rate is `0.1 if ENV['ENABLE_SENTRY_TRANSACTIONS']`,
so "set at run time". OpenTelemetry SDK and exporter, configured in `lib/opentelemetry_config.rb`.
`@sentry/vue` in the browser, no replays. No agent in the repository.

The first run said OpenTelemetry was "not configured" and showed the Sentry browser SDK at a file
that only imports it. Changed: `lib` is read (lesson 2), and the table prefers the `.init(` line of
a browser SDK. Not changed: `.env.example` lists `SENTRY_DSN`, `DD_TRACE_AGENT_URL` and others, but
every one is commented out, and the audit does not read a comment. Routes: Sentry copy for
exceptions, Datadog Agent copy for traces (lesson 4), OpenTelemetry logs, Sentry browser copy without
replays.

## forem

Expected: Rails 8.0.5, Ruby 3.3.0. `ddtrace` 1.16 configured. Honeybadger configured. Sentry
configured as a pilot that sends to Better Stack, with a trace rate from `ENV.fetch`. `logtail-rails`
for logs. A Datadog Agent on Heroku: `datadog/prerun.sh` is the script that the Datadog buildpack
runs. Names in `.env_sample`. No Honeycomb gem, although the plan believed so; only a comment in
`prerun.sh` names it.

The first run found no Agent and no env names. Changed: `datadog/prerun.sh` is evidence of the
Datadog Agent, and `.env_sample` and `.env-example` are env files. With the Agent found, logs moved
to "Datadog Agent copy". Exceptions go to "Sentry copy" because Sentry is configured,
although Honeybadger is the main tracker here and Sentry is the pilot. The rule does not know which
is the main one.

## mastodon

Expected: Rails 8.1.4, Ruby 4.0.7. OpenTelemetry SDK, exporter and twelve named instrumentation
gems in a Gemfile group `:opentelemetry`, configured with `c.use_all({...})` in an initializer.
`lograge` in the production group. No error tracker, no browser SDK, no agent.

The first run said "use_all; named instrumentations". The second part was false: it came from
`config.middleware.use` in `config/application.rb` (lesson 1). Changed. The group `:opentelemetry`
is not development or test, so the gems count as production; that is right for Mastodon, which
loads the group when `OTEL_*` is set. Routes: OpenTelemetry already, Rails.error for exceptions.

## discourse

Expected: Rails 8.1.4 by `railties`, Ruby 3.4.7 from the lockfile. `lograge` and nothing else;
Discourse uses its own `logster`, which is not in the vendor table. The near-empty case.

The first run said "No supported stack found". Changed: `railties` in the lockfile is Rails
(lesson 3). The lockfile says `ruby 3.4.7p58`; the audit prints `3.4.7`.

## redmine

Expected: no `Gemfile.lock` in the repository. `gem 'rails', '8.1.4'` in the Gemfile. No
observability at all. The empty case.

The first run said "Rails" with no version. Changed: a Gemfile line that pins one exact version
gives the version. A constraint such as `~> 8.0` still gives none.

## openproject

Expected: Rails 8.1.4, Ruby 4.0.7. Airbrake and AppSignal, both `require: false` and both
configured in initializers. OpenTelemetry with `instrumentation-all` and `c.use_all`. `lograge`. The
plan believed "Sentry or AppSignal"; the Ruby side has no Sentry.

The first run had the same false "named instrumentations" as Mastodon. Changed by the same fix.
Routes: OpenTelemetry already, Rails.error for exceptions with "keeps Airbrake".

## docuseal

Expected: Rails 8.1.3.1, Ruby 4.0.5 from the lockfile. `lograge` only. The plan believed Sentry and
Rollbar; neither is in the Gemfile or the lockfile.

The tool agreed on the first run. Nothing changed.

## Open

These are results that the rules give and that a person can doubt. They are not changed, because
the rules are the plan's and the corpus alone does not say what is right.

- **Chatwoot: which vendor is in use.** See lesson 4. The owner's answer: Chatwoot is a product
  that others host, so it ships every vendor switched off; a customer repository has one in use.
  No rule was added.
- **Packrat: how it sends logs.** Packrat has no OpenTelemetry logs gems, so the audit offers to
  add them. If Packrat sends logs another way, the audit does not see it.
- **Two error trackers in one repository** (Forem) is not what a customer repository looks like.
  The Sentry rule is checked first and its route is right; no rule was added.
- **Commented names in an example env file** are not read (Chatwoot).
- **Browser code outside `app/javascript` and `app/views`** is not read. Discourse and OpenProject
  keep their front end in `frontend/`. OpenProject has `@appsignal/javascript` there; it is not in
  the browser table, so the result is the same today.
