package rails

import (
	"os"
	"strconv"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

const notConfiguredNote = "gem present, no configure call found"

// extra is a line that says more about a configured vendor. SameFile marks a pattern that is too
// common a word to count outside a file that configures the vendor: Mastodon's
// config/application.rb has `config.middleware.use Rack::Attack`.
type extra struct {
	Pattern  string
	Note     string
	SameFile bool
}

// configRule says how a vendor is configured: a call in a config file, or a file that exists.
// Extras are lines that say more once the vendor is configured.
type configRule struct {
	Vendor string
	Calls  []string
	Files  []string
	Extras []extra
}

// configRules is the table of section 4.5 of the plan, with one addition: an OpenTelemetry
// instrumentation installed by hand (`...Instrumentation.instance.install`) counts as configured.
// Packrat itself does this in an initializer and calls SDK.configure from app/lib.
func configRules() []configRule {
	return []configRule{
		{Vendor: "Datadog", Calls: []string{"Datadog.configure"}, Extras: []extra{
			{Pattern: ".tracing.instrument", Note: "tracing instrumented"}, {Pattern: "c.logs", Note: "logs configured", SameFile: true}}},
		{Vendor: "Sentry", Calls: []string{"Sentry.init"}},
		{Vendor: "New Relic", Files: []string{"config/newrelic.yml"}},
		{Vendor: "OpenTelemetry", Calls: []string{"OpenTelemetry::SDK.configure", "Instrumentation.instance.install"}, Extras: []extra{
			{Pattern: "use_all", Note: "use_all"},
			{Pattern: ".use ", Note: "named instrumentations", SameFile: true},
			{Pattern: ".use(", Note: "named instrumentations", SameFile: true},
			{Pattern: "Instrumentation.instance.install", Note: "named instrumentations"},
			{Pattern: "OTEL_EXPORTER_OTLP_ENDPOINT", Note: "reads OTEL_EXPORTER_OTLP_ENDPOINT"}}},
		{Vendor: "Honeybadger", Calls: []string{"Honeybadger.configure"}, Files: []string{"config/honeybadger.yml"}},
		{Vendor: "Bugsnag", Calls: []string{"Bugsnag.configure"}, Files: []string{"config/bugsnag.yml"}},
		{Vendor: "Rollbar", Calls: []string{"Rollbar.configure"}},
		{Vendor: "Airbrake", Calls: []string{"Airbrake.configure"}},
		{Vendor: "Scout", Files: []string{"config/scout_apm.yml"}},
		{Vendor: "Skylight", Files: []string{"config/skylight.yml"}},
		{Vendor: "AppSignal", Calls: []string{"Appsignal.configure"}, Files: []string{"config/appsignal.yml", "config/appsignal.rb"}},
		{Vendor: "Elastic APM", Files: []string{"config/elastic_apm.yml"}},
	}
}

// configSources also reads lib and app/lib. Chatwoot and Packrat call OpenTelemetry::SDK.configure
// from a class there, and an initializer only calls that class.
func configSources(root *os.Root) []source {
	files := list(root, "config", func(base string) bool { return base == "application.rb" })
	files = append(files, list(root, "config/environments", hasExtension(".rb"))...)
	files = append(files, list(root, "config/initializers", hasExtension(".rb"))...)
	files = append(files, walk(root, "lib", hasExtension(".rb"))...)
	files = append(files, walk(root, "app/lib", hasExtension(".rb"))...)
	return readSources(root, files)
}

// configure marks each finding whose vendor is configured. A vendor with no rule is left alone.
func configure(root *os.Root, found []audit.Finding) []audit.Finding {
	sources := configSources(root)
	for _, rule := range configRules() {
		evidence, notes := applyRule(root, rule, sources)
		for i := range found {
			if found[i].Vendor != rule.Vendor {
				continue
			}
			if len(evidence) == 0 {
				found[i].Notes = append(found[i].Notes, notConfiguredNote)
				continue
			}
			found[i].Configured = true
			found[i].Evidence = append(found[i].Evidence, evidence...)
			sortEvidence(found[i].Evidence)
			found[i].Notes = append(found[i].Notes, notes...)
			if rule.Vendor == "Sentry" && notes[0] == sentryTracesOn {
				found[i].Provides = append(append([]string{}, found[i].Provides...), "traces")
			}
		}
	}
	return found
}

func applyRule(root *os.Root, rule configRule, sources []source) ([]audit.Evidence, []string) {
	var evidence []audit.Evidence
	for _, src := range sources {
		for _, call := range rule.Calls {
			if line := firstCodeLine(src, call); line > 0 {
				evidence = append(evidence, audit.Evidence{Path: src.path, Line: line, Text: clip(src.lines[line-1])})
				break
			}
		}
	}
	for _, file := range rule.Files {
		if _, ok := readFile(root, file); ok {
			evidence = append(evidence, audit.Evidence{Path: file})
		}
	}
	if len(evidence) == 0 {
		return nil, nil
	}
	var configuring []source
	for _, src := range sources {
		if hasPath(evidence, src.path) {
			configuring = append(configuring, src)
		}
	}
	var notes []string
	for _, e := range rule.Extras {
		in := sources
		if e.SameFile {
			in = configuring
		}
		if hasCodeLine(in, e.Pattern) && !contains(notes, e.Note) {
			notes = append(notes, e.Note)
		}
	}
	if rule.Vendor == "Sentry" {
		notes = append(notes, sentryTraces(sources))
	}
	return evidence, notes
}

func firstCodeLine(src source, pattern string) int {
	for i, line := range src.lines {
		if !isComment(line) && hasCall(line, pattern) {
			return i + 1
		}
	}
	return 0
}

// hasCall reports whether the line holds the pattern as a whole word at its end. `Sentry.init`
// must not match `Sentry.initialized?`, which Packrat asks in app/lib.
func hasCall(line, pattern string) bool {
	for rest := line; ; {
		at := strings.Index(rest, pattern)
		if at < 0 {
			return false
		}
		rest = rest[at+len(pattern):]
		last := pattern[len(pattern)-1]
		if !isWordByte(last) || rest == "" || !isWordByte(rest[0]) {
			return true
		}
	}
}

func hasCodeLine(sources []source, pattern string) bool {
	for _, src := range sources {
		if firstCodeLine(src, pattern) > 0 {
			return true
		}
	}
	return false
}

func contains(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

const (
	sentryTracesOn      = "traces on"
	sentryTracesOff     = "traces off"
	sentryTracesUnknown = "traces rate set at run time"
)

// sentryTraces reads how Sentry tracing is set. A number above 0, `enable_tracing = true` or a
// `traces_sampler` means on. `nil`, `0`, `false` or no setting means off. Anything else is an
// expression, such as ENV.fetch, that only the running app can answer.
func sentryTraces(sources []source) string {
	state := sentryTracesOff
	for _, src := range sources {
		for _, line := range src.lines {
			if isComment(line) {
				continue
			}
			switch sentryTracesLine(line) {
			case sentryTracesOn:
				return sentryTracesOn
			case sentryTracesUnknown:
				state = sentryTracesUnknown
			}
		}
	}
	return state
}

func sentryTracesLine(line string) string {
	code, _, _ := strings.Cut(line, " #")
	setting, value, assigned := strings.Cut(code, "=")
	value = strings.TrimSpace(value)
	switch {
	case !assigned || strings.HasPrefix(value, "="):
		return ""
	case strings.Contains(setting, "traces_sampler"):
		return sentryTracesOn
	case !strings.Contains(setting, "traces_sample_rate") && !strings.Contains(setting, "enable_tracing"):
		return ""
	case value == "true":
		return sentryTracesOn
	case value == "nil" || value == "false":
		return sentryTracesOff
	}
	rate, err := strconv.ParseFloat(value, 64)
	switch {
	case err != nil:
		return sentryTracesUnknown
	case rate > 0:
		return sentryTracesOn
	}
	return sentryTracesOff
}
