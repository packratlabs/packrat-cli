package rails_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/rails"
)

func gem(vendor, name string, groups ...string) audit.Finding {
	return audit.Finding{Vendor: vendor, Gem: name, Groups: groups}
}

func configured(f audit.Finding, notes ...string) audit.Finding {
	f.Configured = true
	f.Notes = notes
	return f
}

func agent(vendor, path string, line int) audit.Agent {
	return audit.Agent{Vendor: vendor, Evidence: []audit.Evidence{{Path: path, Line: line}}}
}

func facts(parts ...any) audit.Facts {
	var f audit.Facts
	for _, part := range parts {
		switch p := part.(type) {
		case audit.Finding:
			f.SDKs = append(f.SDKs, p)
		case audit.Agent:
			f.Agents = append(f.Agents, p)
		case audit.Browser:
			f.Browser = append(f.Browser, p)
		}
	}
	return f
}

func signal(t *testing.T, f audit.Facts, name audit.SignalName) audit.Signal {
	t.Helper()
	for _, s := range rails.Routes(f) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no %s signal", name)
	return audit.Signal{}
}

var (
	datadog    = gem("Datadog", "datadog")
	ddAgent    = agent("Datadog Agent", "docker-compose.yml", 41)
	otelSDK    = gem("OpenTelemetry", "opentelemetry-sdk")
	otelExport = gem("OpenTelemetry", "opentelemetry-exporter-otlp")
	sentry     = configured(gem("Sentry", "sentry-rails"), "traces off")
	sentryOn   = configured(gem("Sentry", "sentry-rails"), "traces on")
	newRelic   = gem("New Relic", "newrelic_rpm")
	sentryJS   = audit.Browser{Vendor: "Sentry browser"}
	sentryPlay = audit.Browser{Vendor: "Sentry browser", Replays: true}
	datadogRUM = audit.Browser{Vendor: "Datadog RUM", Replays: true}
)

type routeCase struct {
	name    string
	facts   audit.Facts
	route   string
	state   audit.State
	reason  []string
	not     []string
	blocked []string
}

func checkRoutes(t *testing.T, name audit.SignalName, cases []routeCase) {
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
	checkRoutes(t, audit.Traces, []routeCase{
		{name: "1 datadog gem with the Agent in the repository", facts: facts(datadog, ddAgent),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, reason: []string{"Agent config at docker-compose.yml:41"}, blocked: collide},
		{name: "1 datadog gem without an Agent", facts: facts(datadog),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, reason: []string{"Agent config not in this repository; setup will print the lines for it"}, blocked: collide},
		{name: "1 ddtrace is the same vendor", facts: facts(gem("Datadog", "ddtrace")),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, blocked: collide},
		{name: "1 before 2: datadog wins over OpenTelemetry", facts: facts(datadog, otelSDK, otelExport),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, blocked: collide},
		{name: "1 does not apply to a test-only datadog gem", facts: facts(gem("Datadog", "datadog", "test")),
			route: rails.RouteOTelRails, state: audit.StateRoute},
		{name: "1 does not apply to an Agent without the gem", facts: facts(ddAgent),
			route: rails.RouteOTelRails, state: audit.StateRoute},
		{name: "2 SDK and OTLP exporter", facts: facts(otelSDK, otelExport),
			route: rails.RouteOTel, state: audit.StateAlready, reason: []string{"already exports OTLP; setup points the exporter at Packrat"}},
		{name: "2 before 3: New Relic does not change it", facts: facts(otelSDK, otelExport, newRelic, sentryOn),
			route: rails.RouteOTel, state: audit.StateAlready, not: []string{"New Relic", "Sentry"}},
		{name: "2 needs the exporter", facts: facts(otelSDK),
			route: rails.RouteOTelRails, state: audit.StateRoute},
		{name: "2 needs the SDK", facts: facts(otelExport),
			route: rails.RouteOTelRails, state: audit.StateRoute},
		{name: "2 does not apply to a test-only SDK", facts: facts(gem("OpenTelemetry", "opentelemetry-sdk", "test"), otelExport),
			route: rails.RouteOTelRails, state: audit.StateRoute},
		{name: "3 nothing", facts: facts(),
			route: rails.RouteOTelRails, state: audit.StateRoute, not: []string{"New Relic", "Sentry"}},
		{name: "3 with New Relic", facts: facts(newRelic),
			route: rails.RouteOTelRails, state: audit.StateRoute, reason: []string{"coexists with New Relic"}, not: []string{"Sentry"}},
		{name: "3 with Sentry traces on", facts: facts(sentryOn),
			route: rails.RouteOTelRails, state: audit.StateRoute,
			reason: []string{"Sentry traces stay; Packrat reads OpenTelemetry traces because they are finer"}},
		{name: "3 with Sentry traces off", facts: facts(sentry),
			route: rails.RouteOTelRails, state: audit.StateRoute, not: []string{"Sentry"}},
		{name: "3 with New Relic and Sentry traces on", facts: facts(newRelic, sentryOn),
			route: rails.RouteOTelRails, state: audit.StateRoute, reason: []string{"coexists with New Relic", "Sentry traces stay"}},
	})
}

func TestExceptionsRoutes(t *testing.T) {
	checkRoutes(t, audit.Exceptions, []routeCase{
		{name: "1 Sentry configured", facts: facts(sentry),
			route: rails.RouteSentry, state: audit.StateRoute, reason: []string{"sentry-ruby sends; Packrat takes the same envelope"}},
		{name: "1 before 2: Sentry wins over Datadog", facts: facts(sentry, datadog, ddAgent),
			route: rails.RouteSentry, state: audit.StateRoute},
		{name: "1 before 3: Sentry wins over Honeybadger", facts: facts(sentry, gem("Honeybadger", "honeybadger")),
			route: rails.RouteSentry, state: audit.StateRoute, not: []string{"Honeybadger"}},
		{name: "1 does not apply to Sentry that is not configured", facts: facts(gem("Sentry", "sentry-rails")),
			route: rails.RouteOTelErrors, state: audit.StateRoute},
		{name: "1 does not apply to a test-only Sentry", facts: facts(configured(gem("Sentry", "sentry-rails", "test"))),
			route: rails.RouteOTelErrors, state: audit.StateRoute},
		{name: "2 Datadog chosen for traces", facts: facts(datadog),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, reason: []string{"errors arrive as error spans"}},
		{name: "2 before 3: Datadog wins over Rollbar", facts: facts(datadog, gem("Rollbar", "rollbar")),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, not: []string{"Rollbar"}},
		{name: "3 nothing", facts: facts(),
			route: rails.RouteOTelErrors, state: audit.StateRoute, not: []string{"keeps"}},
		{name: "3 keeps Honeybadger", facts: facts(gem("Honeybadger", "honeybadger")),
			route: rails.RouteOTelErrors, state: audit.StateRoute, reason: []string{"keeps Honeybadger"}},
		{name: "3 keeps Bugsnag", facts: facts(gem("Bugsnag", "bugsnag")),
			route: rails.RouteOTelErrors, state: audit.StateRoute, reason: []string{"keeps Bugsnag"}},
		{name: "3 keeps Rollbar", facts: facts(gem("Rollbar", "rollbar")),
			route: rails.RouteOTelErrors, state: audit.StateRoute, reason: []string{"keeps Rollbar"}},
		{name: "3 keeps Airbrake", facts: facts(gem("Airbrake", "airbrake")),
			route: rails.RouteOTelErrors, state: audit.StateRoute, reason: []string{"keeps Airbrake"}},
		{name: "3 does not name a test-only tracker", facts: facts(gem("Rollbar", "rollbar", "test")),
			route: rails.RouteOTelErrors, state: audit.StateRoute, not: []string{"keeps"}},
		{name: "3 with OpenTelemetry already here", facts: facts(otelSDK, otelExport),
			route: rails.RouteOTelErrors, state: audit.StateRoute},
	})
}

func TestLogsRoutes(t *testing.T) {
	later := "Ruby OpenTelemetry logs are pre-1.0; traces first"
	checkRoutes(t, audit.Logs, []routeCase{
		{name: "1 datadog gem and the Agent", facts: facts(datadog, ddAgent),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, reason: []string{"same Agent"}},
		{name: "1 needs the Agent", facts: facts(datadog),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later}},
		{name: "1 needs the gem", facts: facts(ddAgent),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later}},
		{name: "1 needs the Datadog Agent, not another collector", facts: facts(datadog, agent("Vector", "vector.toml", 1)),
			route: rails.RouteOTelLogs, state: audit.StateLater},
		{name: "1 before 2: lograge does not change it", facts: facts(datadog, ddAgent, gem("logging", "lograge")),
			route: rails.RouteDatadogAgent, state: audit.StateRoute, not: []string{"lograge"}},
		{name: "2 nothing", facts: facts(),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later}, not: []string{"shaped"}},
		{name: "2 names lograge", facts: facts(gem("logging", "lograge")),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later, "lograge"}},
		{name: "2 names semantic_logger", facts: facts(gem("logging", "semantic_logger")),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later, "semantic_logger"}},
		{name: "2 with the OpenTelemetry logs gems is still later", facts: facts(otelSDK, gem("OpenTelemetry", "opentelemetry-logs-sdk")),
			route: rails.RouteOTelLogs, state: audit.StateLater, reason: []string{later}},
	})
}

func TestReplaysRoutes(t *testing.T) {
	checkRoutes(t, audit.Replays, []routeCase{
		{name: "1 Sentry browser with replays", facts: facts(sentryPlay),
			route: rails.RouteSentryBrowser, state: audit.StateRoute, reason: []string{"replays on"}},
		{name: "1 before 3: Sentry wins over Datadog RUM", facts: facts(datadogRUM, sentryPlay),
			route: rails.RouteSentryBrowser, state: audit.StateRoute, reason: []string{"replays on"}},
		{name: "2 Sentry browser without replays", facts: facts(sentryJS),
			route: rails.RouteSentryBrowser, state: audit.StateRoute, reason: []string{"replays not enabled yet"}},
		{name: "2 before 3: Sentry without replays wins over Datadog RUM", facts: facts(datadogRUM, sentryJS),
			route: rails.RouteSentryBrowser, state: audit.StateRoute, reason: []string{"replays not enabled yet"}},
		{name: "3 Datadog RUM", facts: facts(datadogRUM),
			route: rails.RouteNone, state: audit.StateUnsupported, reason: []string{"Datadog RUM replays are not supported yet"}},
		{name: "4 nothing", facts: facts(),
			route: rails.RouteSentryNew, state: audit.StateRoute, reason: []string{"Packrat accepts Sentry envelopes"}},
		{name: "4 another browser vendor is nothing", facts: facts(audit.Browser{Vendor: "LogRocket", Replays: true}),
			route: rails.RouteSentryNew, state: audit.StateRoute},
		{name: "4 the Sentry gem is not the browser SDK", facts: facts(sentry),
			route: rails.RouteSentryNew, state: audit.StateRoute},
	})
}

func TestRoutesAreFourInAFixedOrder(t *testing.T) {
	var names []audit.SignalName
	for _, s := range rails.Routes(audit.Facts{}) {
		names = append(names, s.Name)
	}
	want := []audit.SignalName{audit.Exceptions, audit.Traces, audit.Logs, audit.Replays}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("signals %v, want %v", names, want)
	}
}
