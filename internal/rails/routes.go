package rails

import (
	"fmt"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/detect"
)

const (
	vendorPackratRails  = "packrat-rails"
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
// is the order of precedence. Rule 1 of exceptions, traces and logs, packrat-rails, is the owner's
// and not the plan's, so the plan's rules are one higher here. Logs differs from the plan; see
// logsRoute. A gem that loads only in development or test picks no route.
func Routes(facts detect.Facts) []detect.Signal {
	datadog := hasVendor(facts, vendorDatadog)
	signals := make([]detect.Signal, 0, 4)
	for _, name := range []detect.SignalName{detect.Exceptions, detect.Traces, detect.Logs, detect.Replays} {
		var signal detect.Signal
		switch name {
		case detect.Exceptions:
			signal = exceptionsRoute(facts, datadog)
		case detect.Traces:
			signal = tracesRoute(facts, datadog)
		case detect.Logs:
			signal = logsRoute(facts, datadog)
		case detect.Replays:
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

func tracesRoute(facts detect.Facts, datadog bool) detect.Signal {
	// Traces 1: packrat-rails sends them already.
	if hasVendor(facts, vendorPackratRails) {
		return detect.Signal{Route: detect.RoutePackratRails, State: detect.StateAlready, Reason: "packrat-rails sends traces to Packrat"}
	}
	// Traces 2: the datadog gem decides, because it cannot share Net::HTTP with OpenTelemetry.
	if datadog {
		reason := "Agent config not in this repository; setup will print the lines for it"
		if where := agentWhere(facts); where != "" {
			reason = "Agent config at " + where
		}
		return detect.Signal{Route: detect.RouteDatadogAgent, State: detect.StateRoute, Reason: reason, Blocked: []string{blockedOTel}}
	}
	// Traces 3: the SDK and the OTLP exporter are both here.
	if hasGem(facts, "opentelemetry-sdk") && hasGem(facts, "opentelemetry-exporter-otlp") {
		return detect.Signal{Route: detect.RouteOTel, State: detect.StateAlready,
			Reason: "already exports OTLP; setup adds Packrat as a second OTLP destination, and the current one keeps receiving"}
	}
	// Traces 4: everything else gets OpenTelemetry.
	reasons := []string{"setup adds the OpenTelemetry SDK"}
	if hasGem(facts, "newrelic_rpm") {
		reasons = append(reasons, "coexists with New Relic")
	}
	if sentryTracesAreOn(facts) {
		reasons = append(reasons, "Sentry traces stay; Packrat reads OpenTelemetry traces because they are finer")
	}
	return detect.Signal{Route: detect.RouteOTelRails, State: detect.StateRoute, Reason: strings.Join(reasons, "; ")}
}

func exceptionsRoute(facts detect.Facts, datadog bool) detect.Signal {
	// Exceptions 1: packrat-rails subscribes to Rails.error and sends what it reports.
	if hasVendor(facts, vendorPackratRails) {
		reason := "packrat-rails sends Rails.error to Packrat"
		if sentryConfigured(facts) {
			reason += "; Sentry keeps its own"
		}
		return detect.Signal{Route: detect.RoutePackratRails, State: detect.StateAlready, Reason: reason}
	}
	// Exceptions 2: a configured Sentry wins over Datadog.
	if sentryConfigured(facts) {
		return detect.Signal{Route: detect.RouteSentry, State: detect.StateRoute,
			Reason: "sentry-ruby sends; Packrat takes the same envelope"}
	}
	// Exceptions 3: the route chosen for traces carries the errors.
	if datadog {
		return detect.Signal{Route: detect.RouteDatadogAgent, State: detect.StateRoute, Reason: "errors arrive as error spans"}
	}
	// Exceptions 4: Rails.error, next to any error tracker that is here.
	reasons := []string{"setup subscribes to Rails.error"}
	for _, vendor := range []string{"Honeybadger", "Bugsnag", "Rollbar", "Airbrake"} {
		if hasVendor(facts, vendor) {
			reasons = append(reasons, "keeps "+vendor)
		}
	}
	return detect.Signal{Route: detect.RouteOTelErrors, State: detect.StateRoute, Reason: strings.Join(reasons, "; ")}
}

func logsRoute(facts detect.Facts, datadog bool) detect.Signal {
	// Logs 1: packrat-rails sends them already.
	if hasVendor(facts, vendorPackratRails) {
		return detect.Signal{Route: detect.RoutePackratRails, State: detect.StateAlready, Reason: "packrat-rails sends logs to Packrat"}
	}
	// Logs 2: only when the Agent is in the repository; the gem alone does not ship logs.
	if datadog && agentWhere(facts) != "" {
		return detect.Signal{Route: detect.RouteDatadogAgent, State: detect.StateRoute, Reason: "same Agent"}
	}
	// Logs 3 and 4 are not the plan's. The plan said "later" for every other case, because Ruby
	// OpenTelemetry logs are pre-1.0. The owner decided after the corpus to treat them as released:
	// Packrat accepts them, and Colorbook sends its logs this way.
	// Logs 3: the logs SDK and its OTLP exporter are both here.
	if hasGem(facts, "opentelemetry-logs-sdk") && hasGem(facts, "opentelemetry-exporter-otlp-logs") {
		return detect.Signal{Route: detect.RouteOTelLogs, State: detect.StateAlready,
			Reason: "already exports OTLP logs; setup adds Packrat as a second OTLP destination, and the current one keeps receiving"}
	}
	// Logs 4: everything else gets OpenTelemetry logs.
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
	return detect.Signal{Route: detect.RouteOTelLogs, State: detect.StateRoute, Reason: reason}
}

func replaysRoute(facts detect.Facts) detect.Signal {
	for _, browser := range facts.Browser {
		if browser.Vendor != vendorSentryBrowser {
			continue
		}
		// Replays 1: Sentry browser with replays on.
		if browser.Replays {
			return detect.Signal{Route: detect.RouteSentryBrowser, State: detect.StateRoute, Reason: "replays on"}
		}
		// Replays 2: Sentry browser without replays.
		return detect.Signal{Route: detect.RouteSentryBrowser, State: detect.StateRoute, Reason: "replays not enabled yet"}
	}
	// Replays 3: Datadog RUM.
	for _, browser := range facts.Browser {
		if browser.Vendor == vendorDatadogRUM {
			return detect.Signal{Route: detect.RouteNone, State: detect.StateUnsupported,
				Reason: "Datadog RUM replays are not supported yet"}
		}
	}
	// Replays 4: nothing.
	return detect.Signal{Route: detect.RouteSentryNew, State: detect.StateRoute, Reason: "Packrat accepts Sentry envelopes"}
}

func hasVendor(facts detect.Facts, vendor string) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendor && sdk.Production() {
			return true
		}
	}
	return false
}

func hasGem(facts detect.Facts, gem string) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Gem == gem && sdk.Production() {
			return true
		}
	}
	return false
}

func sentryConfigured(facts detect.Facts) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendorSentry && sdk.Production() && sdk.Configured {
			return true
		}
	}
	return false
}

func sentryTracesAreOn(facts detect.Facts) bool {
	for _, sdk := range facts.SDKs {
		if sdk.Vendor == vendorSentry && sdk.Production() && contains(sdk.Notes, sentryTracesOn) {
			return true
		}
	}
	return false
}

// agentWhere returns the first place the Datadog Agent was found, or nothing.
func agentWhere(facts detect.Facts) string {
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
