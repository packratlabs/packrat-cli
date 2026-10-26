package rails_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/detect"
	"github.com/packratlabs/packrat-cli/internal/rails"
)

func gem(vendor, name string, groups ...string) detect.Finding {
	return detect.Finding{Vendor: vendor, Gem: name, Groups: groups}
}

func configured(f detect.Finding, notes ...string) detect.Finding {
	f.Configured = true
	f.Notes = notes
	return f
}

func agent(vendor, path string, line int) detect.Agent {
	return detect.Agent{Vendor: vendor, Evidence: []detect.Evidence{{Path: path, Line: line}}}
}

func facts(parts ...any) detect.Facts {
	var f detect.Facts
	for _, part := range parts {
		switch p := part.(type) {
		case detect.Finding:
			f.SDKs = append(f.SDKs, p)
		case detect.Agent:
			f.Agents = append(f.Agents, p)
		case detect.Browser:
			f.Browser = append(f.Browser, p)
		}
	}
	return f
}

func signal(t *testing.T, f detect.Facts, name detect.SignalName) detect.Signal {
	t.Helper()
	for _, s := range rails.Routes(f) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no %s signal", name)
	return detect.Signal{}
}

var (
	datadog        = gem("Datadog", "datadog")
	ddAgent        = agent("Datadog Agent", "docker-compose.yml", 41)
	otelSDK        = gem("OpenTelemetry", "opentelemetry-sdk")
	otelExport     = gem("OpenTelemetry", "opentelemetry-exporter-otlp")
	otelLogs       = gem("OpenTelemetry", "opentelemetry-logs-sdk")
	otelLogsExport = gem("OpenTelemetry", "opentelemetry-exporter-otlp-logs")
	sentry         = configured(gem("Sentry", "sentry-rails"), "traces off")
	sentryOn       = configured(gem("Sentry", "sentry-rails"), "traces on")
	newRelic       = gem("New Relic", "newrelic_rpm")
	sentryJS       = detect.Browser{Vendor: "Sentry browser"}
	sentryPlay     = detect.Browser{Vendor: "Sentry browser", Replays: true}
	datadogRUM     = detect.Browser{Vendor: "Datadog RUM", Replays: true}
	packratRails   = gem("packrat-rails", "packrat-rails")
)

type routeCase struct {
	name    string
	facts   detect.Facts
	route   string
	state   detect.State
	reason  []string
	not     []string
	blocked []string
}

func checkRoutes(t *testing.T, name detect.SignalName, cases []routeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := signal(t, tc.facts, name)
			if got.Route != tc.route || got.State != tc.state {
				t.Errorf("route %q state %q, want %q %q", got.Route, got.State, tc.route, tc.state)
			}
			for _, part := range tc.reason {
				if !strings.Contains(got.Reason, part) {
					t.Errorf("reason %q lacks %q", got.Reason, part)
				}
			}
			for _, part := range tc.not {
				if strings.Contains(got.Reason, part) {
					t.Errorf("reason %q must not hold %q", got.Reason, part)
				}
			}
			want := tc.blocked
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got.Blocked, want) {
				t.Errorf("blocked %v, want %v", got.Blocked, want)
			}
		})
	}
}

func TestTracesRoutes(t *testing.T) {
	collide := []string{"OpenTelemetry: the datadog gem and the OpenTelemetry Net::HTTP instrumentation collide"}
	checkRoutes(t, detect.Traces, []routeCase{
		{name: "1 packrat-rails", facts: facts(packratRails, otelSDK, otelExport),
			route: detect.RoutePackratRails, state: detect.StateAlready, reason: []string{"packrat-rails sends traces to Packrat"}},
		{name: "1 before 2: packrat-rails wins over datadog", facts: facts(packratRails, datadog, ddAgent),
			route: detect.RoutePackratRails, state: detect.StateAlready},
		{name: "1 does not apply to a test-only packrat-rails", facts: facts(gem("packrat-rails", "packrat-rails", "test")),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "2 datadog gem with the Agent in the repository", facts: facts(datadog, ddAgent),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, reason: []string{"Agent config at docker-compose.yml:41"}, blocked: collide},
		{name: "2 datadog gem without an Agent", facts: facts(datadog),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, reason: []string{"Agent config not in this repository; setup will print the lines for it"}, blocked: collide},
		{name: "2 ddtrace is the same vendor", facts: facts(gem("Datadog", "ddtrace")),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, blocked: collide},
		{name: "2 before 3: datadog wins over OpenTelemetry", facts: facts(datadog, otelSDK, otelExport),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, blocked: collide},
		{name: "2 does not apply to a test-only datadog gem", facts: facts(gem("Datadog", "datadog", "test")),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "2 does not apply to an Agent without the gem", facts: facts(ddAgent),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "3 SDK and OTLP exporter", facts: facts(otelSDK, otelExport),
			route: detect.RouteOTel, state: detect.StateAlready, reason: []string{"already exports OTLP; setup adds Packrat as a second OTLP destination, and the current one keeps receiving"}},
		{name: "3 before 4: New Relic does not change it", facts: facts(otelSDK, otelExport, newRelic, sentryOn),
			route: detect.RouteOTel, state: detect.StateAlready, not: []string{"New Relic", "Sentry"}},
		{name: "3 needs the exporter", facts: facts(otelSDK),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "3 needs the SDK", facts: facts(otelExport),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "3 does not apply to a test-only SDK", facts: facts(gem("OpenTelemetry", "opentelemetry-sdk", "test"), otelExport),
			route: detect.RouteOTelRails, state: detect.StateRoute},
		{name: "4 nothing", facts: facts(),
			route: detect.RouteOTelRails, state: detect.StateRoute, not: []string{"New Relic", "Sentry"}},
		{name: "4 with New Relic", facts: facts(newRelic),
			route: detect.RouteOTelRails, state: detect.StateRoute, reason: []string{"coexists with New Relic"}, not: []string{"Sentry"}},
		{name: "4 with Sentry traces on", facts: facts(sentryOn),
			route: detect.RouteOTelRails, state: detect.StateRoute,
			reason: []string{"Sentry traces stay; Packrat reads OpenTelemetry traces because they are finer"}},
		{name: "4 with Sentry traces off", facts: facts(sentry),
			route: detect.RouteOTelRails, state: detect.StateRoute, not: []string{"Sentry"}},
		{name: "4 with New Relic and Sentry traces on", facts: facts(newRelic, sentryOn),
			route: detect.RouteOTelRails, state: detect.StateRoute, reason: []string{"coexists with New Relic", "Sentry traces stay"}},
	})
}

func TestExceptionsRoutes(t *testing.T) {
	checkRoutes(t, detect.Exceptions, []routeCase{
		{name: "1 packrat-rails", facts: facts(packratRails),
			route: detect.RoutePackratRails, state: detect.StateAlready, reason: []string{"packrat-rails sends Rails.error to Packrat"}, not: []string{"Sentry"}},
		{name: "1 before 2: packrat-rails wins over Sentry, which keeps its copy", facts: facts(packratRails, sentry),
			route: detect.RoutePackratRails, state: detect.StateAlready, reason: []string{"Sentry keeps its own"}},
		{name: "1 does not apply to a test-only packrat-rails", facts: facts(gem("packrat-rails", "packrat-rails", "test"), sentry),
			route: detect.RouteSentry, state: detect.StateRoute},
		{name: "2 Sentry configured", facts: facts(sentry),
			route: detect.RouteSentry, state: detect.StateRoute, reason: []string{"sentry-ruby sends; Packrat takes the same envelope"}},
		{name: "2 before 3: Sentry wins over Datadog", facts: facts(sentry, datadog, ddAgent),
			route: detect.RouteSentry, state: detect.StateRoute},
		{name: "2 before 4: Sentry wins over Honeybadger", facts: facts(sentry, gem("Honeybadger", "honeybadger")),
			route: detect.RouteSentry, state: detect.StateRoute, not: []string{"Honeybadger"}},
		{name: "2 does not apply to Sentry that is not configured", facts: facts(gem("Sentry", "sentry-rails")),
			route: detect.RouteOTelErrors, state: detect.StateRoute},
		{name: "2 does not apply to a test-only Sentry", facts: facts(configured(gem("Sentry", "sentry-rails", "test"))),
			route: detect.RouteOTelErrors, state: detect.StateRoute},
		{name: "3 Datadog chosen for traces", facts: facts(datadog),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, reason: []string{"errors arrive as error spans"}},
		{name: "3 before 4: Datadog wins over Rollbar", facts: facts(datadog, gem("Rollbar", "rollbar")),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, not: []string{"Rollbar"}},
		{name: "4 nothing", facts: facts(),
			route: detect.RouteOTelErrors, state: detect.StateRoute, not: []string{"keeps"}},
		{name: "4 keeps Honeybadger", facts: facts(gem("Honeybadger", "honeybadger")),
			route: detect.RouteOTelErrors, state: detect.StateRoute, reason: []string{"keeps Honeybadger"}},
		{name: "4 keeps Bugsnag", facts: facts(gem("Bugsnag", "bugsnag")),
			route: detect.RouteOTelErrors, state: detect.StateRoute, reason: []string{"keeps Bugsnag"}},
		{name: "4 keeps Rollbar", facts: facts(gem("Rollbar", "rollbar")),
			route: detect.RouteOTelErrors, state: detect.StateRoute, reason: []string{"keeps Rollbar"}},
		{name: "4 keeps Airbrake", facts: facts(gem("Airbrake", "airbrake")),
			route: detect.RouteOTelErrors, state: detect.StateRoute, reason: []string{"keeps Airbrake"}},
		{name: "4 does not name a test-only tracker", facts: facts(gem("Rollbar", "rollbar", "test")),
			route: detect.RouteOTelErrors, state: detect.StateRoute, not: []string{"keeps"}},
		{name: "4 with OpenTelemetry already here", facts: facts(otelSDK, otelExport),
			route: detect.RouteOTelErrors, state: detect.StateRoute},
	})
}

func TestLogsRoutes(t *testing.T) {
	added := "setup adds the OpenTelemetry logs SDK"
	checkRoutes(t, detect.Logs, []routeCase{
		{name: "1 packrat-rails", facts: facts(packratRails, otelLogs, otelLogsExport),
			route: detect.RoutePackratRails, state: detect.StateAlready, reason: []string{"packrat-rails sends logs to Packrat"}},
		{name: "1 before 2: packrat-rails wins over the Agent", facts: facts(packratRails, datadog, ddAgent),
			route: detect.RoutePackratRails, state: detect.StateAlready},
		{name: "2 datadog gem and the Agent", facts: facts(datadog, ddAgent),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, reason: []string{"same Agent"}},
		{name: "2 needs the Agent", facts: facts(datadog),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}},
		{name: "2 needs the gem", facts: facts(ddAgent),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}},
		{name: "2 needs the Datadog Agent, not another collector", facts: facts(datadog, agent("Vector", "vector.toml", 1)),
			route: detect.RouteOTelLogs, state: detect.StateRoute},
		{name: "2 before 4: lograge does not change it", facts: facts(datadog, ddAgent, gem("logging", "lograge")),
			route: detect.RouteDatadogAgent, state: detect.StateRoute, not: []string{"lograge"}},
		{name: "2 before 3: the Agent wins over the OpenTelemetry logs gems", facts: facts(datadog, ddAgent, otelLogs, otelLogsExport),
			route: detect.RouteDatadogAgent, state: detect.StateRoute},
		{name: "3 logs SDK and OTLP logs exporter", facts: facts(otelLogs, otelLogsExport),
			route: detect.RouteOTelLogs, state: detect.StateAlready, reason: []string{"already exports OTLP logs; setup adds Packrat as a second OTLP destination, and the current one keeps receiving"}},
		{name: "3 before 4: lograge does not change it", facts: facts(otelLogs, otelLogsExport, gem("logging", "lograge")),
			route: detect.RouteOTelLogs, state: detect.StateAlready, not: []string{"lograge", added}},
		{name: "3 needs the exporter", facts: facts(otelSDK, otelLogs),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}},
		{name: "3 needs the logs SDK", facts: facts(otelLogsExport),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}},
		{name: "3 does not apply to the trace exporter", facts: facts(otelSDK, otelExport),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}},
		{name: "3 does not apply to test-only logs gems", facts: facts(gem("OpenTelemetry", "opentelemetry-logs-sdk", "test"), otelLogsExport),
			route: detect.RouteOTelLogs, state: detect.StateRoute},
		{name: "4 nothing", facts: facts(),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added}, not: []string{"shaped"}},
		{name: "4 names lograge", facts: facts(gem("logging", "lograge")),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added, "lograge"}},
		{name: "4 names semantic_logger", facts: facts(gem("logging", "semantic_logger")),
			route: detect.RouteOTelLogs, state: detect.StateRoute, reason: []string{added, "semantic_logger"}},
	})
}

func TestReplaysRoutes(t *testing.T) {
	checkRoutes(t, detect.Replays, []routeCase{
		{name: "1 Sentry browser with replays", facts: facts(sentryPlay),
			route: detect.RouteSentryBrowser, state: detect.StateRoute, reason: []string{"replays on"}},
		{name: "1 before 3: Sentry wins over Datadog RUM", facts: facts(datadogRUM, sentryPlay),
			route: detect.RouteSentryBrowser, state: detect.StateRoute, reason: []string{"replays on"}},
		{name: "2 Sentry browser without replays", facts: facts(sentryJS),
			route: detect.RouteSentryBrowser, state: detect.StateRoute, reason: []string{"replays not enabled yet"}},
		{name: "2 before 3: Sentry without replays wins over Datadog RUM", facts: facts(datadogRUM, sentryJS),
			route: detect.RouteSentryBrowser, state: detect.StateRoute, reason: []string{"replays not enabled yet"}},
		{name: "3 Datadog RUM", facts: facts(datadogRUM),
			route: detect.RouteNone, state: detect.StateUnsupported, reason: []string{"Datadog RUM replays are not supported yet"}},
		{name: "4 nothing", facts: facts(),
			route: detect.RouteSentryNew, state: detect.StateRoute, reason: []string{"Packrat accepts Sentry envelopes"}},
		{name: "4 another browser vendor is nothing", facts: facts(detect.Browser{Vendor: "LogRocket", Replays: true}),
			route: detect.RouteSentryNew, state: detect.StateRoute},
		{name: "4 the Sentry gem is not the browser SDK", facts: facts(sentry),
			route: detect.RouteSentryNew, state: detect.StateRoute},
	})
}

func TestRoutesAreFourInAFixedOrder(t *testing.T) {
	var names []detect.SignalName
	for _, s := range rails.Routes(detect.Facts{}) {
		names = append(names, s.Name)
	}
	want := []detect.SignalName{detect.Exceptions, detect.Traces, detect.Logs, detect.Replays}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("signals %v, want %v", names, want)
	}
}
