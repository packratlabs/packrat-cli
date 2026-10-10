// Package report prints an Audit as a plain-text table or as JSON.
package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// NoStack is the line printed when the repository is not a supported stack.
const NoStack = "No supported stack found (Rails is the only one yet)"

// Promise is always the last line of the table.
const Promise = "Nothing was changed. Nothing was sent."

// FoundHeading is printed above the evidence in the verbose table.
const FoundHeading = "Found in this repository"

// Hint is printed by the short table when there is evidence that it does not show.
const Hint = "Add --verbose to see what was found and why."

// HostHeading is printed above the facts of --host, so that nobody reads them as production.
const HostHeading = "This machine, not production"

const (
	maxWidth     = 99
	continuation = "      "
	// A repository with opentelemetry-instrumentation-all locks about fifty of these as
	// dependencies. Named one by one they would bury every other finding, so the table counts
	// them. The JSON lists them all.
	instrumentationPrefix = "opentelemetry-instrumentation-"
)

// row is one finding: a first line of three columns, then lines that hold a list.
type row struct {
	vendor, where, what, notes string
	more                       []string
}

// Text writes the short answer: one line for each signal. It is what a person reads first.
func Text(w io.Writer, a audit.Audit) error {
	return write(w, a, false)
}

// Verbose writes the short answer, the reason for each line, and everything that was found.
func Verbose(w io.Writer, a audit.Audit) error {
	return write(w, a, true)
}

func write(w io.Writer, a audit.Audit, verbose bool) error {
	var b strings.Builder
	b.WriteString(header(a) + "\n")
	if a.Stack == nil {
		b.WriteString("\n" + NoStack + "\n")
	}
	if len(a.Notes) > 0 {
		b.WriteString("\n" + strings.Join(a.Notes, "\n") + "\n")
	}
	found := foundRows(a)
	hint := !verbose && len(found) > 0
	if len(a.Signals) > 0 {
		b.WriteString("\n")
		writeRows(&b, signalRows(a, verbose))
		b.WriteString("\n" + summary(a) + "\n")
	} else if hint {
		b.WriteString("\n")
	}
	if hint {
		b.WriteString(Hint + "\n")
	}
	if verbose {
		b.WriteString("\n" + FoundHeading + "\n")
		writeRows(&b, found)
	}
	if a.Host != nil {
		b.WriteString("\n" + HostHeading + "\n")
		writeRows(&b, hostRows(*a.Host))
	}
	b.WriteString("\n" + Promise + "\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

func foundRows(a audit.Audit) []row {
	rows := sdkRows(a.SDKs)
	for _, agent := range a.Agents {
		rows = append(rows, agentRow(agent))
	}
	for _, browser := range a.Browser {
		rows = append(rows, browserRow(browser))
	}
	if len(a.EnvNames) > 0 {
		rows = append(rows, envRow(a.EnvNames))
	}
	return rows
}

func header(a audit.Audit) string {
	head := "packrat audit  " + filepath.Base(a.Path)
	if a.Stack == nil {
		return head
	}
	parts := []string{strings.TrimSpace("Rails " + a.Stack.Version)}
	if a.Stack.Ruby != "" {
		parts = append(parts, "Ruby "+a.Stack.Ruby)
	}
	return head + "  " + strings.Join(parts, ", ")
}

// sdkRows makes one row for each vendor. The list is sorted by vendor, so a vendor's gems are
// next to each other.
func sdkRows(sdks []audit.Finding) []row {
	var rows []row
	for start := 0; start < len(sdks); {
		end := start
		for end < len(sdks) && sdks[end].Vendor == sdks[start].Vendor {
			end++
		}
		rows = append(rows, vendorRow(sdks[start:end]))
		start = end
	}
	return rows
}

func vendorRow(gems []audit.Finding) row {
	shown := production(gems)
	r := row{vendor: gems[0].Vendor, where: where(configEvidence(shown[0]))}
	var provides, notes, names []string
	instrumentations := 0
	for _, gem := range gems {
		if !gem.Direct && strings.HasPrefix(gem.Gem, instrumentationPrefix) {
			instrumentations++
			continue
		}
		names = append(names, gemLabel(gem))
	}
	for _, gem := range shown {
		provides = union(provides, gem.Provides)
		notes = union(notes, gem.Notes)
	}
	switch instrumentations {
	case 0:
	case 1:
		names = append(names, "1 instrumentation gem (dependency)")
	default:
		names = append(names, fmt.Sprintf("%d instrumentation gems (dependency)", instrumentations))
	}
	r.what = strings.Join(provides, ", ")
	if !shown[0].Production() {
		r.what += only(shown[0])
	}
	r.notes = strings.Join(notes, "; ")
	r.more = wrap(names, ", ")
	return r
}

// production returns the gems that load in production, or every gem when none does.
func production(gems []audit.Finding) []audit.Finding {
	var kept []audit.Finding
	for _, gem := range gems {
		if gem.Production() {
			kept = append(kept, gem)
		}
	}
	if len(kept) == 0 {
		return gems
	}
	return kept
}

func gemLabel(gem audit.Finding) string {
	label := strings.TrimSpace(gem.Gem + " " + gem.Version)
	if !gem.Direct {
		label += " (dependency)"
	}
	if !gem.Production() {
		label += only(gem)
	}
	return label
}

func only(gem audit.Finding) string {
	return " (" + strings.Join(gem.Groups, ", ") + " only)"
}

// configEvidence prefers the line that configures the gem to the line that locks it.
func configEvidence(gem audit.Finding) []audit.Evidence {
	for i, e := range gem.Evidence {
		if e.Path != "Gemfile" && e.Path != "Gemfile.lock" {
			return gem.Evidence[i:]
		}
	}
	return gem.Evidence
}

func where(evidence []audit.Evidence) string {
	switch {
	case len(evidence) == 0:
		return ""
	case evidence[0].Line == 0:
		return evidence[0].Path
	}
	return fmt.Sprintf("%s:%d", evidence[0].Path, evidence[0].Line)
}

func agentRow(agent audit.Agent) row {
	r := row{vendor: agent.Vendor, where: where(agent.Evidence)}
	var others []string
	for i := 1; i < len(agent.Evidence); i++ {
		others = append(others, where(agent.Evidence[i:]))
	}
	if len(others) > 0 {
		r.what = "also " + strings.Join(others, ", ")
	}
	return r
}

func browserRow(browser audit.Browser) row {
	replays := "no replays found"
	if browser.Replays {
		replays = "replays on"
	}
	what := replays
	if browser.Package != "" {
		what = browser.Package + "; " + replays
	}
	return row{vendor: browser.Vendor, where: where(initEvidence(browser.Evidence)), what: what}
}

// initEvidence prefers the line that starts the SDK. Chatwoot imports @sentry/vue in ten files,
// and the first of them by name only reports an error.
func initEvidence(evidence []audit.Evidence) []audit.Evidence {
	for i, e := range evidence {
		if strings.Contains(e.Text, ".init(") {
			return evidence[i:]
		}
	}
	return evidence
}

func envRow(names []audit.EnvName) row {
	var all, files []string
	for _, name := range names {
		all = append(all, name.Name)
		files = union(files, name.Files)
	}
	sort.Strings(files)
	return row{vendor: "Env names", what: "in " + strings.Join(files, ", "), more: wrap(all, ", ")}
}

const (
	statusReady       = "ready"
	statusSetup       = "needs setup"
	statusLater       = "later"
	statusUnsupported = "not supported"
)

// signalRows makes one line for each signal: its name, how much work it is, then what the work is, in
// words that do not need the route names. The verbose table adds the route and its reason.
func signalRows(a audit.Audit, verbose bool) []row {
	var rows []row
	for _, signal := range a.Signals {
		name := string(signal.Name)
		r := row{vendor: strings.ToUpper(name[:1]) + name[1:], where: status(a, signal), what: sentence(a, signal)}
		if verbose {
			r.more = wrap(strings.Fields(signal.Route+": "+signal.Reason), " ")
			for _, blocked := range signal.Blocked {
				r.more = append(r.more, wrap(strings.Fields("not "+blocked), " ")...)
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// status says how much work a signal is. "ready" means that Packrat can receive what the
// repository already sends, with no new library.
func status(a audit.Audit, signal audit.Signal) string {
	switch signal.State {
	case audit.StateAlready:
		return statusReady
	case audit.StateLater:
		return statusLater
	case audit.StateUnsupported:
		return statusUnsupported
	case audit.StateRoute:
		if signal.Route == audit.RouteSentry || (signal.Route == audit.RouteSentryBrowser && sentryReplays(a)) {
			return statusReady
		}
	}
	return statusSetup
}

func sentryReplays(a audit.Audit) bool {
	for _, browser := range a.Browser {
		if browser.Vendor == "Sentry browser" && browser.Replays {
			return true
		}
	}
	return false
}

func sentence(a audit.Audit, signal audit.Signal) string {
	if signal.State == audit.StateUnsupported || signal.State == audit.StateLater {
		return signal.Reason
	}
	ready := status(a, signal) == statusReady
	switch signal.Route {
	case audit.RouteSentry:
		return "through Sentry, which you already use"
	case audit.RouteDatadogAgent:
		return "through your Datadog Agent, which sends a copy"
	case audit.RouteOTel:
		return "through OpenTelemetry, which you already use"
	case audit.RouteOTelRails:
		return "add the OpenTelemetry gems"
	case audit.RouteOTelErrors:
		return "add an OpenTelemetry subscriber for Rails errors"
	case audit.RouteOTelLogs:
		if ready {
			return "through OpenTelemetry, which you already use"
		}
		return "add the OpenTelemetry logs gems"
	case audit.RouteSentryBrowser:
		if ready {
			return "through the Sentry browser SDK, which you already use"
		}
		return "turn on replays in the Sentry browser SDK, which you already use"
	case audit.RouteSentryNew:
		return "add the Sentry browser SDK"
	}
	return signal.Route
}

func summary(a audit.Audit) string {
	ready := 0
	for _, signal := range a.Signals {
		if status(a, signal) == statusReady {
			ready++
		}
	}
	if ready == 1 {
		return fmt.Sprintf("1 of %d signals is ready.", len(a.Signals))
	}
	return fmt.Sprintf("%d of %d signals are ready.", ready, len(a.Signals))
}

func hostRows(host audit.Host) []row {
	listener := "nothing listens"
	if host.TraceListener {
		listener = "something listens"
	}
	rows := []row{
		{vendor: "Datadog Agent files", where: orNone(host.AgentPaths)},
		{vendor: "localhost:8126", where: listener},
		{vendor: "Env names set", where: orNone(host.EnvSet)},
	}
	for _, note := range host.Notes {
		rows = append(rows, row{vendor: "Note", where: note})
	}
	return rows
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func union(list, added []string) []string {
	for _, item := range added {
		present := false
		for _, each := range list {
			present = present || each == item
		}
		if !present {
			list = append(list, item)
		}
	}
	return list
}

func writeRows(b *strings.Builder, rows []row) {
	if len(rows) == 0 {
		b.WriteString("  nothing\n")
		return
	}
	vendorWidth, whereWidth := 0, 0
	for _, r := range rows {
		vendorWidth = max(vendorWidth, len(r.vendor))
		whereWidth = max(whereWidth, len(r.where))
	}
	for _, r := range rows {
		left := fmt.Sprintf("  %-*s", vendorWidth, r.vendor)
		if r.where != "" {
			left += fmt.Sprintf("  %-*s", whereWidth, r.where)
		}
		for _, line := range append(fit(left, r.what, r.notes), r.more...) {
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
}

// fit keeps a row under 100 columns. The notes move to their own lines first, then the rest.
func fit(left, what, notes string) []string {
	line := left + "  " + what
	full := line
	switch {
	case notes != "" && what != "":
		full = line + "; " + notes
	case notes != "":
		full = line + notes
	}
	switch {
	case len(full) <= maxWidth:
		return []string{full}
	case len(line) <= maxWidth:
		return append([]string{line}, wrap(strings.Split(notes, "; "), "; ")...)
	}
	return append([]string{left}, wrap(strings.Split(strings.TrimPrefix(full, left+"  "), ", "), ", ")...)
}

// wrap joins items into continuation lines and breaks only between items.
func wrap(items []string, separator string) []string {
	var wrapped []string
	line := continuation
	for i, item := range items {
		if i < len(items)-1 {
			item += strings.TrimRight(separator, " ")
		}
		if len(line) > len(continuation) && len(line)+1+len(item) > maxWidth {
			wrapped = append(wrapped, line)
			line = continuation
		}
		if len(line) > len(continuation) {
			line += " "
		}
		line += item
	}
	if len(line) > len(continuation) {
		wrapped = append(wrapped, line)
	}
	return wrapped
}
