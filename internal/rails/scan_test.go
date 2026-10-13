package rails

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/detect"
)

func places(evidence []detect.Evidence) []string {
	var all []string
	for _, e := range evidence {
		all = append(all, fmt.Sprintf("%s:%d", e.Path, e.Line))
	}
	return all
}

func TestConfigured(t *testing.T) {
	tests := []struct {
		fixture, gem string
		configured   bool
		places       []string
		provides     []string
		notes        []string
	}{
		{"sentry-traces-off", "sentry-rails", true, []string{"Gemfile.lock:5", "config/initializers/sentry.rb:2"},
			[]string{"exceptions"}, []string{sentryTracesOff}},
		{"sentry-traces-at-run-time", "sentry-rails", true, []string{"Gemfile.lock:5", "config/initializers/sentry.rb:1"},
			[]string{"exceptions"}, []string{sentryTracesUnknown}},
		{"canary", "sentry-rails", true, []string{"Gemfile.lock:6", "config/initializers/sentry.rb:1"},
			[]string{"exceptions", "traces"}, []string{sentryTracesOn}},
		{"canary", "datadog", true, []string{"Gemfile.lock:4", "config/initializers/datadog.rb:1"},
			[]string{"traces", "logs", "exceptions"}, []string{"needs an Agent; no agentless APM in Ruby", "tracing instrumented"}},
		{"gem-not-configured", "rollbar", false, []string{"Gemfile.lock:6"},
			[]string{"exceptions"}, []string{notConfiguredNote}},
		{"gem-not-configured", "lograge", false, []string{"Gemfile.lock:4"},
			[]string{"logs shape"}, []string{"not a destination"}},
		{"configured-by-file", "newrelic_rpm", true, []string{"Gemfile.lock:4", "config/newrelic.yml:0"},
			[]string{"traces", "exceptions", "logs"}, []string{"coexists with OpenTelemetry"}},
		{"configured-by-file", "skylight", false, []string{"Gemfile.lock:6"},
			[]string{"traces"}, []string{notConfiguredNote}},
		{"otel-use-all", "opentelemetry-sdk", true, []string{"Gemfile.lock:6", "config/initializers/opentelemetry.rb:1"},
			[]string{"traces"}, []string{"use_all", "reads OTEL_EXPORTER_OTLP_ENDPOINT"}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture+"/"+tc.gem, func(t *testing.T) {
			facts, err := Stack{}.Read(openFixture(t, tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range facts.SDKs {
				if f.Gem != tc.gem {
					continue
				}
				if f.Configured != tc.configured || !reflect.DeepEqual(places(f.Evidence), tc.places) ||
					!reflect.DeepEqual(f.Provides, tc.provides) || !reflect.DeepEqual(f.Notes, tc.notes) {
					t.Errorf("got configured=%v %v %v %q", f.Configured, places(f.Evidence), f.Provides, f.Notes)
				}
				return
			}
			t.Errorf("no finding for %s", tc.gem)
		})
	}
}

func TestSentryTracesLine(t *testing.T) {
	tests := map[string]string{
		"  config.traces_sample_rate = 0.2":           sentryTracesOn,
		"  config.traces_sample_rate = 1":             sentryTracesOn,
		"  config.traces_sample_rate = 0":             sentryTracesOff,
		"  config.traces_sample_rate = 0.0 # later":   sentryTracesOff,
		"  config.traces_sample_rate = nil":           sentryTracesOff,
		"  config.traces_sample_rate = ENV['R'].to_f": sentryTracesUnknown,
		"  config.enable_tracing = true":              sentryTracesOn,
		"  config.enable_tracing = false":             sentryTracesOff,
		"  config.traces_sampler = ->(ctx) { 0.1 }":   sentryTracesOn,
		"  if config.traces_sample_rate == 1":         "",
		"  config.dsn = ENV['SENTRY_DSN']":            "",
		"  config.profiles_sample_rate = 1.0":         "",
	}
	for line, want := range tests {
		if got := sentryTracesLine(line); got != want {
			t.Errorf("%q: got %q, want %q", line, got, want)
		}
	}
}

func TestAgents(t *testing.T) {
	tests := []struct {
		fixture string
		want    map[string][]string
	}{
		{"agent-by-script", map[string][]string{"Datadog Agent": {"Procfile:1", "bin/with-datadog:3"}}},
		{"no-stack-agent-and-names", map[string][]string{"OpenTelemetry Collector": {"docker-compose.yml:3"}}},
		{"canary", map[string][]string{
			"Datadog Agent": {".github/workflows/ci.yml:10", "Dockerfile:6", "Procfile:1", "bin/with-agent:3",
				"config/deploy.yml:11", "docker-compose.yml:6"},
			"OpenTelemetry Collector": {".github/workflows/ci.yml:10", "k8s/agent.yaml:8"},
			"Vector":                  {"k8s/agent.yaml:11"},
		}},
		{"direct-and-transitive", map[string][]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			got := map[string][]string{}
			for _, agent := range agents(openFixture(t, tc.fixture)) {
				got[agent.Vendor] = places(agent.Evidence)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBrowser(t *testing.T) {
	type found struct {
		vendor, pack string
		replays      bool
		places       []string
	}
	tests := []struct {
		fixture string
		want    []found
	}{
		{"browser-sample-in-pre", []found{}},
		{"browser-importmap-replays", []found{
			{"Sentry browser", "@sentry/browser", true, []string{"app/javascript/application.js:1", "app/javascript/application.js:3",
				"app/javascript/application.js:5", "config/importmap.rb:2"}},
		}},
		{"browser-datadog-rum", []found{
			{"Datadog RUM", "@datadog/browser-rum", true, []string{"app/javascript/rum.ts:1", "app/javascript/rum.ts:3", "package.json:3"}},
			{"PostHog browser", "posthog-js", false, []string{"package.json:4"}},
		}},
		{"canary", []found{
			{"Sentry browser", "", true, []string{"app/views/layouts/application.html.erb:1", "app/views/layouts/application.html.erb:2"}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			got := []found{}
			for _, b := range browser(openFixture(t, tc.fixture)) {
				got = append(got, found{b.Vendor, b.Package, b.Replays, places(b.Evidence)})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestEnvNames(t *testing.T) {
	tests := []struct {
		fixture string
		want    []string
	}{
		{"no-stack-agent-and-names", []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "SENTRY_DSN"}},
		{"canary", []string{
			"APPSIGNAL_PUSH_API_KEY", "BUGSNAG_API_KEY", "DD_AGENT_HOST", "DD_API_KEY", "DD_ENV", "DD_SERVICE", "DD_SITE",
			"DD_VERSION", "ELASTIC_APM_SECRET_TOKEN", "HONEYBADGER_API_KEY", "NEW_RELIC_LICENSE_KEY",
			"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_SERVICE_NAME", "PACKRAT_INGEST_KEY", "POSTHOG_API_KEY",
			"ROLLBAR_ACCESS_TOKEN", "SCOUT_KEY", "SENTRY_DSN", "SENTRY_ENVIRONMENT", "SENTRY_PRIVATE_KEY",
		}},
		{"empty", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			got := []string{}
			for _, name := range envNames(openFixture(t, tc.fixture)) {
				got = append(got, name.Name)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAssignedName(t *testing.T) {
	tests := map[string]string{
		"SENTRY_DSN=secret":               "SENTRY_DSN",
		"export SENTRY_DSN=secret":        "SENTRY_DSN",
		"  SENTRY_DSN: secret":            "SENTRY_DSN",
		"  - SENTRY_DSN":                  "SENTRY_DSN",
		"  - SENTRY_DSN=secret":           "SENTRY_DSN",
		`    "SENTRY_DSN": "secret",`:     "SENTRY_DSN",
		"ENV SENTRY_DSN secret":           "SENTRY_DSN",
		"ENV SENTRY_DSN=secret":           "SENTRY_DSN",
		"  SENTRY_DSN = \"secret\"":       "SENTRY_DSN",
		"SENTRY_DSN":                      "",
		"DATABASE_URL=secret":             "",
		"sentry_dsn=secret":               "",
		"SENTRY_=secret":                  "",
		"OTHER=SENTRY_DSN":                "",
		"ENV OTHER SENTRY_DSN":            "",
		"name: SENTRY_DSN":                "",
		"SENTRY_DSN-OLD=secret":           "",
		"run: SENTRY_DSN=secret ./deploy": "",
	}
	for line, want := range tests {
		if got := assignedName(line); got != want {
			t.Errorf("%q: got %q, want %q", line, got, want)
		}
	}
}

func TestNamesOnly(t *testing.T) {
	yes := []string{".env", ".env.production", ".env.example", "app.json", "fly.toml", "Procfile", "Dockerfile",
		"Dockerfile.dev", "docker-compose.yml", "docker-compose.prod.yaml", "compose.yml", "config/deploy.yml",
		".github/workflows/ci.yml"}
	no := []string{"Gemfile.lock", "config/initializers/sentry.rb", "bin/with-datadog", "k8s/agent.yaml",
		"app/views/layouts/application.html.erb", "config/app.json", "vendor/.env"}
	for _, name := range yes {
		if !NamesOnly(name) {
			t.Errorf("%s must be names-only", name)
		}
	}
	for _, name := range no {
		if NamesOnly(name) {
			t.Errorf("%s must not be names-only", name)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openDir(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func TestSymlinkIsNotRead(t *testing.T) {
	outside, repo := t.TempDir(), t.TempDir()
	writeFile(t, outside, "secret.env", "SENTRY_DSN=secret\n")
	writeFile(t, outside, "views/page.html.erb", "<script>Sentry.init({})</script>\n")
	writeFile(t, repo, "app/views/home.html.erb", "<p>home</p>\n")
	if err := os.Symlink(filepath.Join(outside, "secret.env"), filepath.Join(repo, ".env")); err != nil {
		t.Skipf("this system does not make symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "views"), filepath.Join(repo, "app", "views", "linked")); err != nil {
		t.Fatal(err)
	}
	root := openDir(t, repo)
	if names := envNames(root); len(names) != 0 {
		t.Errorf("read names through a symlink: %v", names)
	}
	if found := browser(root); len(found) != 0 {
		t.Errorf("walked through a symlink: %v", found)
	}
}

func TestLargeFileAndSkippedDirectoriesAreNotRead(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, ".env", "SENTRY_DSN=x\n"+strings.Repeat("#", maxFileSize))
	writeFile(t, repo, "app/javascript/node_modules/@sentry/browser/index.js", "Sentry.init({})\n")
	writeFile(t, repo, "app/javascript/ok.js", "console.log(1)\n")
	root := openDir(t, repo)
	if names := envNames(root); len(names) != 0 {
		t.Errorf("read a file over 2 MB: %v", names)
	}
	if found := browser(root); len(found) != 0 {
		t.Errorf("walked into node_modules: %v", found)
	}
}

func TestHasCall(t *testing.T) {
	tests := []struct {
		line, pattern string
		want          bool
	}{
		{"Sentry.init do |config|", "Sentry.init", true},
		{"  Sentry.init", "Sentry.init", true},
		{"Sentry.init(dsn: x)", "Sentry.init", true},
		{"return unless Sentry.initialized? && user", "Sentry.init", false},
		{"Sentry.initialized? && Sentry.init { }", "Sentry.init", true},
		{"c.use_all({})", "use_all", true},
		{"c.use_all_the_things", "use_all", false},
		{"config.middleware.use Rack::Attack", ".use ", true},
		{"c.use(\"OpenTelemetry::Instrumentation::Rack\")", ".use(", true},
		{"nothing here", "Sentry.init", false},
	}
	for _, tc := range tests {
		if got := hasCall(tc.line, tc.pattern); got != tc.want {
			t.Errorf("hasCall(%q, %q) = %v, want %v", tc.line, tc.pattern, got, tc.want)
		}
	}
}

func TestExactVersion(t *testing.T) {
	tests := map[string]string{
		`gem 'rails', '8.1.4'`:                  "8.1.4",
		`gem "rails", "8.0.5" # Ruby on Rails`:  "8.0.5",
		`gem "rails", "~> 8.0"`:                 "",
		`gem "rails", ">= 8.1.3.1"`:             "",
		`gem "rails", "~> 8.1.3", ">= 8.1.3.1"`: "",
		`gem "rails"`:                           "",
		`gem "rails", require: false`:           "",
		`gem "rails", github: "rails/rails"`:    "",
	}
	for line, want := range tests {
		if got := exactVersion(line); got != want {
			t.Errorf("exactVersion(%q) = %q, want %q", line, got, want)
		}
	}
}
