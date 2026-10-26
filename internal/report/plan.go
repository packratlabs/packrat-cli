package report

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/packratlabs/packrat-cli/internal/detect"
	"github.com/packratlabs/packrat-cli/internal/query"
)

// PlanSchema names the JSON document of packrat plan.
const PlanSchema = "packrat.plan/1"

// NoPlan is printed when the repository is not a supported stack.
const NoPlan = NoStack + ". There is no plan."

// Installed marks a source that the repository already has.
const Installed = "✓"

// The vendor names of the facts, and the source names that packrat status prints.
const (
	packratRails  = "packrat-rails"
	datadog       = "Datadog"
	datadogAgent  = "Datadog Agent"
	openTelemetry = "OpenTelemetry"
	sentry        = "Sentry"
	sentryBrowser = "Sentry browser"
)

// AppHint is printed when plan read the repository alone, which cannot show where a source sends.
const AppHint = "Add --app service/env to detect if Packrat is receiving data from each signal."

const (
	stepArrives       = "none: Packrat receives it"
	stepPackratRails  = "none: packrat-rails sends it"
	stepDualShip      = "dual-ship to Packrat"
	stepBrowserCopy   = "copy to Packrat with a DSN"
	stepDualShipAgent = "dual-ship from the Agent"
	stepUnsupported   = "not supported"
)

// Step is what setup would do for one signal. Source is what Packrat would receive it from, and
// Installed says that the repository has that source now.
type Step struct {
	Name      detect.SignalName `json:"name"`
	Source    string            `json:"source"`
	Installed bool              `json:"installed"`
	Step      string            `json:"step"`
	Route     string            `json:"route"`
	State     detect.State      `json:"state"`
	Reason    string            `json:"reason"`
	Blocked   []string          `json:"blocked"`
	// Arrives is true when Packrat already receives the signal and keeps it. Only --app can know.
	Arrives bool `json:"arrives"`
}

type planDocument struct {
	Schema string            `json:"schema"`
	Path   string            `json:"path"`
	App    *string           `json:"app"`
	Stack  *detect.StackInfo `json:"stack"`
	Steps  []Step            `json:"steps"`
	Notes  []string          `json:"notes"`
}

// Planned is false when there is no supported stack, and so no plan.
func Planned(a detect.Result) bool {
	return a.Stack != nil
}

// Steps turns the route of each signal into what setup would do. With the status of an app, a
// signal that Packrat already keeps has no step, and its source is the one that sends it now: a
// second source of exceptions or of traces would not be kept.
func Steps(a detect.Result, status *query.Status) []Step {
	steps := make([]Step, 0, len(a.Signals))
	for _, signal := range a.Signals {
		step := Step{Name: signal.Name, Route: signal.Route, State: signal.State, Reason: signal.Reason, Blocked: signal.Blocked}
		step.Source, step.Installed, step.Step = plan(a, signal)
		if kept := keptSources(status, signal.Name); len(kept) > 0 {
			step.Source, step.Installed, step.Step, step.Arrives = strings.Join(kept, ", "), true, stepArrives, true
			step.Reason, step.Blocked = "Packrat receives "+string(signal.Name)+" from "+strings.Join(kept, " and "), []string{}
		}
		steps = append(steps, step)
	}
	return steps
}

func keptSources(status *query.Status, name detect.SignalName) []string {
	if status == nil {
		return nil
	}
	var kept []string
	for _, signal := range status.Signals {
		if signal.Signal != string(name) {
			continue
		}
		for _, source := range signal.Sources {
			if source.Kept && source.LastSeenAt != nil {
				kept = append(kept, source.Name)
			}
		}
	}
	return kept
}

func plan(a detect.Result, signal detect.Signal) (source string, installed bool, step string) {
	switch signal.Route {
	case detect.RoutePackratRails:
		if signal.Name == detect.Exceptions && sentryConfigured(a) {
			return packratRails + " + " + sentry, true, stepPackratRails
		}
		return packratRails, true, stepPackratRails
	case detect.RouteSentry:
		return sentry, true, stepDualShip
	case detect.RouteDatadogAgent:
		return datadog, true, stepDualShipAgent
	case detect.RouteOTel:
		return openTelemetry, true, stepDualShip
	case detect.RouteOTelRails:
		return openTelemetry, false, "add the OpenTelemetry gems"
	case detect.RouteOTelErrors:
		return openTelemetry, hasOpenTelemetry(a), "subscribe to Rails.error"
	case detect.RouteOTelLogs:
		if signal.State == detect.StateAlready {
			return openTelemetry, true, stepDualShip
		}
		return openTelemetry, false, "add the OpenTelemetry logs gems"
	case detect.RouteSentryBrowser:
		if sentryReplays(a) {
			return sentryBrowser, true, stepBrowserCopy
		}
		return sentryBrowser, true, "turn on replays, then " + stepBrowserCopy
	case detect.RouteSentryNew:
		return sentryBrowser, false, "add the Sentry browser SDK"
	}
	return None, false, stepUnsupported
}

func hasOpenTelemetry(a detect.Result) bool {
	for _, sdk := range a.SDKs {
		if sdk.Vendor == openTelemetry && sdk.Production() && provides(sdk, string(detect.Traces)) {
			return true
		}
	}
	return false
}

// sentryConfigured decides whether Sentry is named beside packrat-rails, in detect and in plan.
func sentryConfigured(a detect.Result) bool {
	for _, sdk := range a.SDKs {
		if sdk.Vendor == sentry && sdk.Production() && sdk.Configured {
			return true
		}
	}
	return false
}

func sentryReplays(a detect.Result) bool {
	for _, browser := range a.Browser {
		if browser.Vendor == sentryBrowser && browser.Replays {
			return true
		}
	}
	return false
}

// Plan writes one line for each signal: its source, whether it is installed, and the step.
func Plan(w io.Writer, a detect.Result) error {
	return PlanFor(w, a, nil, false)
}

// PlanVerbose adds the reason for each step.
func PlanVerbose(w io.Writer, a detect.Result) error {
	return PlanFor(w, a, nil, true)
}

// PlanJSON writes the plan document.
func PlanJSON(w io.Writer, a detect.Result) error {
	return PlanJSONFor(w, a, nil)
}

// PlanJSONFor writes the plan document for an app whose status is known, or for the repository alone.
func PlanJSONFor(w io.Writer, a detect.Result, status *query.Status) error {
	notes := a.Notes
	if notes == nil {
		notes = []string{}
	}
	var app *string
	if status != nil {
		app = &status.App.App
	}
	return putJSON(w, planDocument{Schema: PlanSchema, Path: a.Path, App: app, Stack: a.Stack, Steps: Steps(a, status), Notes: notes})
}

// PlanFor writes the plan for an app whose status is known, or for the repository alone.
func PlanFor(w io.Writer, a detect.Result, status *query.Status, verbose bool) error {
	var b strings.Builder
	b.WriteString(heading("plan", a) + "\n\n")
	if !Planned(a) {
		b.WriteString(NoPlan + "\n")
		return put(w, b.String())
	}
	steps := Steps(a, status)
	sourceWidth, total, arrive := len("SOURCE"), 0, 0
	for _, step := range steps {
		sourceWidth = max(sourceWidth, len(step.Source))
		switch {
		case step.Arrives:
			arrive++
		case step.Source != None && step.Route != detect.RoutePackratRails:
			total++
		}
	}
	line := func(name, source, mark, step string) {
		pad := strings.Repeat(" ", len("INSTALLED")-utf8.RuneCountInString(mark))
		b.WriteString(strings.TrimRight(fmt.Sprintf("  %-10s  %-*s  %s%s  %s", name, sourceWidth, source, mark, pad, step), " ") + "\n")
	}
	line("SIGNAL", "SOURCE", "INSTALLED", "STEP")
	var notes []string
	for _, step := range steps {
		mark := ""
		if step.Installed {
			mark = Installed
		}
		line(label(step.Name), step.Source, mark, step.Step)
		if verbose && step.Source != None {
			for _, reason := range wrap(strings.Fields(step.Reason), " ") {
				b.WriteString(reason + "\n")
			}
		}
		if step.Source == None {
			notes = append(notes, label(step.Name)+": "+step.Reason+".")
		}
		for _, blocked := range step.Blocked {
			notes = append(notes, label(step.Name)+" do not use "+blocked+".")
		}
	}
	for _, note := range notes {
		b.WriteString("\n")
		for _, wrapped := range wrap(strings.Fields(note), " ") {
			b.WriteString("  " + strings.TrimPrefix(wrapped, continuation) + "\n")
		}
	}
	if status == nil {
		b.WriteString("\n" + AppHint + "\n")
	} else {
		b.WriteString("\n" + planSummary(total, arrive) + "\n")
	}
	return put(w, b.String())
}

// planSummary is printed only when the status of the app is known.
func planSummary(total, arrive int) string {
	receives := fmt.Sprintf("Packrat already receives %s.", count(arrive, "signal"))
	switch {
	case arrive == 0:
		return count(total, "step") + ". Packrat receives none of these signals yet."
	case total == 0:
		return "Nothing to do. " + receives
	default:
		return count(total, "step") + ". " + receives
	}
}
