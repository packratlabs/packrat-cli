package rails

import (
	"fmt"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// The routes of version 1.
const (
	RouteDatadogAgent  = "Datadog Agent copy"
	RouteOTel          = "OpenTelemetry"
	RouteOTelRails     = "OpenTelemetry (Rails)"
	RouteSentry        = "Sentry copy"
	RouteOTelErrors    = "OpenTelemetry exceptions (Rails.error)"
	RouteOTelLogs      = "OpenTelemetry logs"
	RouteSentryBrowser = "Sentry browser copy"
	RouteSentryNew     = "Sentry browser SDK pointed at Packrat"
	RouteNone          = "none"
)

const (
	vendorDatadog       = "Datadog"
	vendorDatadogAgent  = "Datadog Agent"
	vendorSentry        = "Sentry"
	vendorSentryBrowser = "Sentry browser"
	vendorDatadogRUM    = "Datadog RUM"
	vendorLogging       = "logging"

	blockedOTel = "OpenTelemetry: the datadog gem and the OpenTelemetry Net::HTTP instrumentation collide"
)

// Routes picks a route for each of the four signals. It reads nothing; the facts are its only
// input. The rule numbers in the comments are those of section 5.1 of the plan, and their order
// is the order of precedence. Logs differs from the plan; see logsRoute. A gem that loads only in development or test picks no route.
func Routes(facts audit.Facts) []audit.Signal {
	datadog := hasVendor(facts, vendorDatadog)
	signals := make([]audit.Signal, 0, 4)
	for _, name := range []audit.SignalName{audit.Exceptions, audit.Traces, audit.Logs, audit.Replays} {
		var signal audit.Signal
		switch name {
		case audit.Exceptions:
			signal = exceptionsRoute(facts, datadog)
		case audit.Traces:
			signal = tracesRoute(facts, datadog)
		case audit.Logs:
			signal = logsRoute(facts, datadog)
		case audit.Replays:
			signal = replaysRoute(facts)
		}
		signal.Name = name
		if signal.Blocked == nil {
			signal.Blocked = []string{}
		}
		signals = append(signals, signal)
	}
	return signals
}

func tracesRoute(facts audit.Facts, datadog bool) audit.Signal {
	// Traces 1: the datadog gem decides, because it cannot share Net::HTTP with OpenTelemetry.
	if datadog {
		reason := "Agent config not in this repository; setup will print the lines for it"
		if where := agentWhere(facts); where != "" {
			reason = "Agent config at " + where
		}
		return audit.Signal{Route: RouteDatadogAgent, State: audit.StateRoute, Reason: reason, Blocked: []string{blockedOTel}}
	}
	// Traces 2: the SDK and the OTLP exporter are both here.
	if hasGem(facts, "opentelemetry-sdk") && hasGem(facts, "opentelemetry-exporter-otlp") {
		return audit.Signal{Route: RouteOTel, State: audit.StateAlready,
			Reason: "already exports OTLP; setup points the exporter at Packrat"}
	}
	// Traces 3: everything else gets OpenTelemetry.
	reasons := []string{"setup adds the OpenTelemetry SDK"}
	if hasGem(facts, "newrelic_rpm") {
		reasons = append(reasons, "coexists with New Relic")
	}
	if sentryTracesAreOn(facts) {
		reasons = append(reasons, "Sentry traces stay; Packrat reads OpenTelemetry traces because they are finer")
	}
	return audit.Signal{Route: RouteOTelRails, State: audit.StateRoute, Reason: strings.Join(reasons, "; ")}
}

func exceptionsRoute(facts audit.Facts, datadog bool) audit.Signal {
	// Exceptions 1: a configured Sentry wins over Datadog.
	if sentryConfigured(facts) {
		return audit.Signal{Route: RouteSentry, State: audit.StateRoute,
			Reason: "sentry-ruby sends; Packrat takes the same envelope"}
	}
	// Exceptions 2: the route chosen for traces carries the errors.
	if datadog {
		return audit.Signal{Route: RouteDatadogAgent, State: audit.StateRoute, Reason: "errors arrive as error spans"}
	}
	// Exceptions 3: Rails.error, next to any error tracker that is here.
	reasons := []string{"setup subscribes to Rails.error"}
	for _, vendor := range []string{"Honeybadger", "Bugsnag", "Rollbar", "Airbrake"} {
		if hasVendor(facts, vendor) {
			reasons = append(reasons, "keeps "+vendor)
		}
	}
	return audit.Signal{Route: RouteOTelErrors, State: audit.StateRoute, Reason: strings.Join(reasons, "; ")}
}

func logsRoute(facts audit.Facts, datadog bool) audit.Signal {
	// Logs 1: only when the Agent is in the repository; the gem alone does not ship logs.
	if datadog && agentWhere(facts) != "" {
		return audit.Signal{Route: RouteDatadogAgent, State: audit.StateRoute, Reason: "same Agent"}
	}
	// Logs 2 and 3 are not the plan's. The plan said "later" for every other case, because Ruby
	// OpenTelemetry logs are pre-1.0. The owner decided after the corpus to treat them as released:
	// Packrat accepts them, and Colorbook sends its logs this way.
	// Logs 2: the logs SDK and its OTLP exporter are both here.
	if hasGem(facts, "opentelemetry-logs-sdk") && hasGem(facts, "opentelemetry-exporter-otlp-logs") {
		return audit.Signal{Route: RouteOTelLogs, State: audit.StateAlready,
			Reason: "already exports OTLP logs; setup points the exporter at Packrat"}
	}
	// Logs 3: everything else gets OpenTelemetry logs.
	reason := "setup adds the OpenTelemetry logs SDK"
	var gems []string
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendorLogging && sdk.Production() {
			gems = append(gems, sdk.Gem)
		}
	}
	if len(gems) > 0 {
		reason += "; logs are shaped by " + strings.Join(gems, ", ")
	}
	return audit.Signal{Route: RouteOTelLogs, State: audit.StateRoute, Reason: reason}
}

func replaysRoute(facts audit.Facts) audit.Signal {
	for _, browser := range facts.Browser {
		if browser.Vendor != vendorSentryBrowser {
			continue
		}
		// Replays 1: Sentry browser with replays on.
		if browser.Replays {
			return audit.Signal{Route: RouteSentryBrowser, State: audit.StateRoute, Reason: "replays on"}
		}
		// Replays 2: Sentry browser without replays.
		return audit.Signal{Route: RouteSentryBrowser, State: audit.StateRoute, Reason: "replays not enabled yet"}
	}
	// Replays 3: Datadog RUM.
	for _, browser := range facts.Browser {
		if browser.Vendor == vendorDatadogRUM {
			return audit.Signal{Route: RouteNone, State: audit.StateUnsupported,
				Reason: "Datadog RUM replays are not supported yet"}
		}
	}
	// Replays 4: nothing.
	return audit.Signal{Route: RouteSentryNew, State: audit.StateRoute, Reason: "Packrat accepts Sentry envelopes"}
}

func hasVendor(facts audit.Facts, vendor string) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendor && sdk.Production() {
			return true
		}
	}
	return false
}

func hasGem(facts audit.Facts, gem string) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Gem == gem && sdk.Production() {
			return true
		}
	}
	return false
}

func sentryConfigured(facts audit.Facts) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendorSentry && sdk.Production() && sdk.Configured {
			return true
		}
	}
	return false
}

func sentryTracesAreOn(facts audit.Facts) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendorSentry && sdk.Production() && contains(sdk.Notes, sentryTracesOn) {
			return true
		}
	}
	return false
}

// agentWhere returns the first place the Datadog Agent was found, or nothing.
func agentWhere(facts audit.Facts) string {
	for _, agent := range facts.Agents {
		if agent.Vendor != vendorDatadogAgent || len(agent.Evidence) == 0 {
			continue
		}
		first := agent.Evidence[0]
		if first.Line == 0 {
			return first.Path
		}
		return fmt.Sprintf("%s:%d", first.Path, first.Line)
	}
	return ""
}
