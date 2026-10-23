package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
	"github.com/packratlabs/packrat-cli/internal/report"
)

func TestRun(t *testing.T) {
	empty := filepath.Join("testdata", "empty")
	file := filepath.Join("testdata", "empty", ".gitkeep")
	agent := filepath.Join("testdata", "no-stack-agent-and-names")
	railsOnly := filepath.Join("testdata", "rails-by-railties")
	tests := []struct {
		name       string
		args       []string
		code       int
		stdout     string
		stderr     string
		lastStdout string
		absent     string
	}{
		{name: "empty directory", args: []string{"setup", "detect", empty}, code: 0, stdout: report.NoStack, lastStdout: report.NoStack},
		{name: "flag after path", args: []string{"setup", "detect", empty, "--json"}, code: 0, stdout: `"schema": "packrat.detect/1"`},
		{name: "flag before path", args: []string{"setup", "detect", "--json", empty}, code: 0, stdout: `"schema": "packrat.detect/1"`},
		{name: "no host without the flag", args: []string{"setup", "detect", "--json", empty}, code: 0, stdout: `"host": null`},
		{name: "host flag", args: []string{"setup", "detect", "--host", empty}, code: 0, stdout: report.HostHeading, absent: "Nothing was sent"},
		{name: "host flag with JSON", args: []string{"setup", "detect", "--host", "--json", empty}, code: 0, stdout: `"trace_listener":`},
		{name: "short table has no evidence", args: []string{"setup", "detect", agent}, code: 0, stdout: report.NoStack, absent: "docker-compose.yml"},
		{name: "verbose", args: []string{"setup", "detect", "--verbose", agent}, code: 0, stdout: "docker-compose.yml:3", absent: "Nothing was sent"},
		{name: "verbose, short flag after path", args: []string{"setup", "detect", agent, "-v"}, code: 0, stdout: report.FoundHeading},
		{name: "JSON ignores verbose", args: []string{"setup", "detect", "--json", "-v", agent}, code: 0, stdout: `"vendor": "OpenTelemetry Collector"`, absent: report.FoundHeading},
		{name: "help", args: []string{"--help"}, code: 0, stdout: "CLI to interact with Packrat\n\nVERSION\n  packrat/dev "},
		{name: "help lists the commands", args: []string{"help"}, code: 0, stdout: "COMMANDS\n  apps "},
		{name: "detect help", args: []string{"setup", "detect", "--help"}, code: 0, stdout: "USAGE\n  $ packrat setup detect [PATH]"},
		{name: "help after a path", args: []string{"setup", "detect", empty, "-h"}, code: 0, stdout: "scan a project for existing observability tools\n"},
		{name: "a topic's help", args: []string{"help", "check"}, code: 0, stdout: "COMMANDS\n  check add    schedule a check"},
		{name: "a subcommand's help", args: []string{"check", "close", "--help"}, code: 0, stdout: "USAGE\n  $ packrat check close ID"},
		{name: "setup with no command", args: []string{"setup"}, code: 2, stderr: "specify a command: detect or plan"},
		{name: "the old name is gone", args: []string{"detect"}, code: 2, stderr: `"detect" is not a packrat command`},
		{name: "no help for nothing", args: []string{"help", "nope"}, code: 2, stderr: `no help for "nope"`},
		{name: "plan", args: []string{"setup", "plan", railsOnly}, code: 0, stdout: "Exceptions  OpenTelemetry              subscribe to Rails.error", lastStdout: report.AppHint, absent: "Nothing was sent"},
		{name: "plan, verbose", args: []string{"setup", "plan", "-v", railsOnly}, code: 0, stdout: "setup adds the OpenTelemetry SDK"},
		{name: "plan as JSON", args: []string{"setup", "plan", railsOnly, "--json"}, code: 0, stdout: `"schema": "packrat.plan/1"`},
		{name: "plan with no stack", args: []string{"setup", "plan", empty}, code: 1, stderr: report.NoPlan},
		{name: "plan says that the repository cannot show what arrives", args: []string{"setup", "plan", railsOnly}, code: 0, stdout: report.AppHint},
		{name: "plan has no host flag", args: []string{"setup", "plan", "--host", empty}, code: 2, stderr: "flag provided but not defined"},
		{name: "detect gives no steps", args: []string{"setup", "detect", "--json", railsOnly}, code: 0, stdout: `"sdks"`, absent: `"signals"`},
		{name: "detect names what it found", args: []string{"setup", "detect", railsOnly}, code: 0, stdout: "Exceptions  none", absent: "needs setup"},
		{name: "no command", args: nil, code: 2, stderr: "CLI to interact with Packrat"},
		{name: "unknown command", args: []string{"deploy"}, code: 2, stderr: `"deploy" is not a packrat command. Run packrat help`},
		{name: "unknown flag", args: []string{"setup", "detect", "--yaml"}, code: 2, stderr: "flag provided but not defined"},
		{name: "two paths", args: []string{"setup", "detect", empty, empty}, code: 2, stderr: "specify at most one path"},
		{name: "path is a file", args: []string{"setup", "detect", file}, code: 2, stderr: "not a directory"},
		{name: "path is missing", args: []string{"setup", "detect", filepath.Join("testdata", "absent")}, code: 2, stderr: "not a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout lacks %q:\n%s", tc.stdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
			}
			if tc.absent != "" && strings.Contains(stdout.String(), tc.absent) {
				t.Errorf("stdout must not hold %q:\n%s", tc.absent, stdout.String())
			}
			if tc.lastStdout != "" && !strings.HasSuffix(stdout.String(), tc.lastStdout+"\n") {
				t.Errorf("last line is not %q:\n%s", tc.lastStdout, stdout.String())
			}
			if tc.code != 0 && stdout.Len() != 0 {
				t.Errorf("stdout is not empty on failure:\n%s", stdout.String())
			}
		})
	}
}

// Every command has a summary, a usage line and an example, and each one answers --help.
func TestEveryCommandHasHelp(t *testing.T) {
	for name, c := range commands() {
		if c.summary == "" || !strings.HasPrefix(c.usage, "packrat "+name) || len(c.examples) == 0 {
			t.Errorf("%s: help is missing a summary, a usage line that names it, or an example", name)
		}
		var stdout, stderr bytes.Buffer
		if code := run(append(strings.Fields(name), "--help"), &stdout, &stderr); code != 0 || !strings.HasPrefix(stdout.String(), c.summary+"\n") {
			t.Errorf("%s --help: exit %d\n%s%s", name, code, stdout.String(), stderr.String())
		}
	}
}

func TestJSONEmptyHasNoNullLists(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"setup", "detect", "--json", filepath.Join("testdata", "empty")}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"sdks", "agents", "browser", "env_names", "notes"} {
		if string(doc[key]) != "[]" {
			t.Errorf("%s is %s, want []", key, doc[key])
		}
	}
}

const statusKey = "packrat_api_canary"

const oneApp = `{"now":"2026-10-13T15:00:00Z","apps":[{"app":"shop/production","last_data_at":"2026-10-13T14:59:50Z"}]}`

// answers is a fake of query.packrat.sh that gives the next body of its list on each request to
// /status, and of apps on each request to /apps, and stays on the last one of each.
func answers(t *testing.T, apps []string, bodies ...string) (*httptest.Server, *int) {
	t.Helper()
	asked, listed := 0, 0
	if len(apps) == 0 {
		apps = []string{oneApp}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+statusKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body := apps[min(listed, len(apps)-1)]
		if r.URL.Path == "/status" {
			body = bodies[min(asked, len(bodies)-1)]
			asked++
		} else {
			listed++
		}
		if body == "" {
			w.WriteHeader(http.StatusNotFound)
			body = `{"type":"error","message":"shop/staging is not an app this key opens. This key opens shop/production."}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("answering: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func statusBody(seen string) string {
	return `{"app":"shop/production","now":"2026-10-13T15:00:00Z","last_data_at":"` + seen + `","signals":[` +
		`{"signal":"traces","quiet":false,"sources":[{"source":"otel","name":"OpenTelemetry","kept":true,"last_seen_at":"` + seen + `"}]},` +
		`{"signal":"logs","quiet":false,"sources":[]}],"warnings":[]}`
}

func TestStatusAndApps(t *testing.T) {
	fresh, newer, old := statusBody("2026-10-13T14:59:00Z"), statusBody("2026-10-13T15:00:00Z"), statusBody("2026-10-12T10:00:00Z")
	noApps := `{"now":"2026-10-13T15:00:00Z","apps":[]}`
	devApp := `{"now":"2026-10-13T15:00:00Z","apps":[{"app":"shop/development","last_data_at":"2026-10-13T15:00:00Z"}]}`
	twoApps := `{"now":"2026-10-13T15:00:00Z","apps":[{"app":"shop/production","last_data_at":"2026-10-13T14:59:50Z"},{"app":"shop/development","last_data_at":"2026-10-12T10:00:00Z"}]}`
	twoAppsNewer := `{"now":"2026-10-13T15:00:00Z","apps":[{"app":"shop/production","last_data_at":"2026-10-13T14:59:50Z"},{"app":"shop/development","last_data_at":"2026-10-13T15:00:00Z"}]}`
	tests := []struct {
		name   string
		args   []string
		key    string
		bodies []string
		code   int
		stdout string
		stderr string
		asked  int
		sleeps int
		apps   []string
	}{
		{name: "apps", args: []string{"apps"}, key: statusKey, code: 0, stdout: "APP              LAST DATA\nshop/production  now\n"},
		{name: "apps as JSON", args: []string{"apps", "--json"}, key: statusKey, code: 0, stdout: `"app": "shop/production"`},
		{name: "apps takes no argument", args: []string{"apps", "shop"}, key: statusKey, code: 2, stderr: "takes no argument"},
		{name: "status", args: []string{"status", "--app", "shop/production"}, key: statusKey, bodies: []string{fresh}, code: 0, stdout: "1 of 2 signals is arriving.", asked: 1},
		{name: "status as JSON", args: []string{"status", "--json", "--app=shop/production"}, key: statusKey, bodies: []string{fresh}, code: 0, stdout: `"source": "otel"`, asked: 1},
		{name: "an app that stopped", args: []string{"status", "--app", "shop/production"}, key: statusKey, bodies: []string{old}, code: 1, stdout: "Nothing has arrived for 29 hours.", asked: 1},
		{name: "plan leaves out what the app already sends", args: []string{"setup", "plan", "--app", "shop/production", filepath.Join("testdata", "rails-by-railties")}, key: statusKey, bodies: []string{fresh}, code: 0, stdout: "3 steps. Packrat already receives 1 signal.\n", asked: 1},
		{name: "plan as JSON names the app", args: []string{"setup", "plan", "--json", "--app", "shop/production", filepath.Join("testdata", "rails-by-railties")}, key: statusKey, bodies: []string{fresh}, code: 0, stdout: `"arrives": true`, asked: 1},
		{name: "plan for an app needs a key", args: []string{"setup", "plan", "--app", "shop/production", filepath.Join("testdata", "rails-by-railties")}, code: 2, stderr: "Run packrat login, or set PACKRAT_API_KEY"},
		{name: "plan for an app that is not there", args: []string{"setup", "plan", "--app", "shop/staging", filepath.Join("testdata", "rails-by-railties")}, key: statusKey, bodies: []string{""}, code: 1, stderr: "This key opens shop/production.", asked: 1},
		{name: "no app names the org's one app", args: []string{"status"}, key: statusKey, bodies: []string{fresh}, code: 0, stdout: "1 of 2 signals is arriving.", asked: 1},
		{name: "no app when the org has several", args: []string{"status"}, key: statusKey, apps: []string{twoApps}, code: 2, stderr: "This key has access to shop/production and shop/development."},
		{name: "no app when the org has none", args: []string{"status"}, key: statusKey, apps: []string{noApps}, code: 1, stderr: "no app has sent data yet"},
		{name: "the app as an argument", args: []string{"status", "shop/production"}, key: statusKey, code: 2, stderr: "--app SERVICE/ENV"},
		{name: "wait with no app ends when an app first sends", args: []string{"status", "--wait"}, key: statusKey, apps: []string{noApps, noApps, devApp}, bodies: []string{fresh}, code: 0, stdout: "Waiting for data from your app.", asked: 1, sleeps: 2},
		{name: "wait with no app ends when a known app sends again", args: []string{"status", "--wait"}, key: statusKey, apps: []string{twoApps, twoAppsNewer}, bodies: []string{fresh}, code: 0, stdout: "OpenTelemetry", asked: 1, sleeps: 1},
		{name: "wait with no app gives up", args: []string{"status", "--wait"}, key: statusKey, apps: []string{twoApps}, code: 1, stderr: "no new data arrived within 5 minutes", sleeps: 100},
		{name: "no key", args: []string{"status", "--app", "shop/production"}, code: 2, stderr: "Run packrat login, or set PACKRAT_API_KEY"},
		{name: "a refused key", args: []string{"status", "--app", "shop/production"}, key: "wrong", bodies: []string{fresh}, code: 1, stderr: "your key was rejected (401)"},
		{name: "an app that is not there", args: []string{"status", "--app", "shop/staging"}, key: statusKey, bodies: []string{""}, code: 1, stderr: "This key opens shop/production.", asked: 1},
		{name: "wait ends when a source changes", args: []string{"status", "--wait", "--app", "shop/production"}, key: statusKey, bodies: []string{fresh, fresh, newer}, code: 0, stdout: "Waiting for data from shop/production.", asked: 3, sleeps: 2},
		{name: "wait ends when the app first appears", args: []string{"status", "--wait", "--app", "shop/production"}, key: statusKey, bodies: []string{"", "", fresh}, code: 0, stdout: "OpenTelemetry", asked: 3, sleeps: 2},
		{name: "wait gives up", args: []string{"status", "--wait", "--app", "shop/production"}, key: statusKey, bodies: []string{fresh}, code: 1, stderr: "no new data arrived within 5 minutes", sleeps: 100, asked: 101},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asked := answers(t, tc.apps, tc.bodies...)
			sleeps := 0
			env := map[string]string{"PACKRAT_API_KEY": tc.key, "PACKRAT_QUERY_URL": server.URL}
			w := world{getenv: func(name string) string { return env[name] }, sleep: func(time.Duration) { sleeps++ },
				store: auth.Store{Dir: t.TempDir()}, http: &http.Client{}}
			var stdout, stderr bytes.Buffer

			code := runIn(w, tc.args, &stdout, &stderr)

			if code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout lacks %q:\n%s", tc.stdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), statusKey) {
				t.Errorf("the key was printed:\n%s%s", stdout.String(), stderr.String())
			}
			if *asked != tc.asked || sleeps != tc.sleeps {
				t.Errorf("asked %d times and slept %d times, want %d and %d", *asked, sleeps, tc.asked, tc.sleeps)
			}
		})
	}
}
