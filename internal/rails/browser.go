package rails

import (
	"os"
	"sort"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// browserPattern is one row of section 4.8 of the plan. A Replay pattern that NeedsVendor counts
// only when another pattern of the vendor matched; its text alone is too common a word.
type browserPattern struct {
	Pattern     string
	Vendor      string
	Package     string
	Replays     bool
	NeedsVendor bool
}

func browserPatterns() []browserPattern {
	const sentry, datadog, posthog = "Sentry browser", "Datadog RUM", "PostHog browser"
	return []browserPattern{
		{Pattern: "@sentry/browser", Vendor: sentry, Package: "@sentry/browser"},
		{Pattern: "@sentry/react", Vendor: sentry, Package: "@sentry/react"},
		{Pattern: "@sentry/vue", Vendor: sentry, Package: "@sentry/vue"},
		{Pattern: "browser.sentry-cdn.com", Vendor: sentry},
		{Pattern: "Sentry.init(", Vendor: sentry},
		{Pattern: "replayIntegration", Vendor: sentry, Replays: true},
		{Pattern: "Sentry.Replay", Vendor: sentry, Replays: true},
		{Pattern: "@sentry/replay", Vendor: sentry, Replays: true},
		{Pattern: "@datadog/browser-rum", Vendor: datadog, Package: "@datadog/browser-rum"},
		{Pattern: "DD_RUM", Vendor: datadog},
		{Pattern: "datadogRum.init", Vendor: datadog},
		{Pattern: "sessionReplay", Vendor: datadog, Replays: true, NeedsVendor: true},
		{Pattern: "startSessionReplayRecording", Vendor: datadog, Replays: true, NeedsVendor: true},
		{Pattern: "posthog-js", Vendor: posthog, Package: "posthog-js"},
		{Pattern: "posthog.init(", Vendor: posthog},
		{Pattern: "logrocket", Vendor: "LogRocket", Package: "logrocket", Replays: true},
		{Pattern: "@highlight-run", Vendor: "Highlight", Package: "@highlight-run", Replays: true},
		{Pattern: "@fullstory", Vendor: "FullStory", Package: "@fullstory", Replays: true},
	}
}

func browserSources(root *os.Root) []source {
	files := list(root, "config", func(base string) bool { return base == "importmap.rb" })
	files = append(files, list(root, ".", func(base string) bool { return base == "package.json" })...)
	files = append(files, walk(root, "app/javascript", hasExtension(".js", ".ts", ".jsx", ".tsx"))...)
	files = append(files, walk(root, "app/views", hasExtension(".erb", ".haml"))...)
	return readSources(root, files)
}

type browserMatch struct {
	pattern  browserPattern
	evidence audit.Evidence
}

func browser(root *os.Root) []audit.Browser {
	var matches []browserMatch
	for _, src := range browserSources(root) {
		matches = append(matches, browserMatchesIn(src)...)
	}
	byVendor := map[string]*audit.Browser{}
	for _, m := range matches {
		if !m.pattern.NeedsVendor {
			addBrowserMatch(byVendor, m)
		}
	}
	for _, m := range matches {
		if m.pattern.NeedsVendor && byVendor[m.pattern.Vendor] != nil {
			addBrowserMatch(byVendor, m)
		}
	}
	found := []audit.Browser{}
	for _, finding := range byVendor {
		sortEvidence(finding.Evidence)
		found = append(found, *finding)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Vendor < found[j].Vendor })
	return found
}

func addBrowserMatch(byVendor map[string]*audit.Browser, m browserMatch) {
	finding := byVendor[m.pattern.Vendor]
	if finding == nil {
		finding = &audit.Browser{Vendor: m.pattern.Vendor}
		byVendor[m.pattern.Vendor] = finding
	}
	if finding.Package == "" {
		finding.Package = m.pattern.Package
	}
	finding.Replays = finding.Replays || m.pattern.Replays
	for _, e := range finding.Evidence {
		if e.Path == m.evidence.Path && e.Line == m.evidence.Line {
			return
		}
	}
	finding.Evidence = append(finding.Evidence, m.evidence)
}

// browserMatchesIn returns the first line of the file for each pattern. A line inside a <pre>
// block of a view is skipped: it is a code sample shown to a reader, not code the page runs.
// Packrat's home page shows `Sentry.init({` this way.
func browserMatchesIn(src source) []browserMatch {
	var matches []browserMatch
	seen := map[string]bool{}
	inPre := false
	for i, line := range src.lines {
		opens, closes := strings.LastIndex(line, "<pre"), strings.LastIndex(line, "</pre>")
		sample := inPre || opens >= 0
		if opens >= 0 || closes >= 0 {
			inPre = opens > closes
		}
		if sample || isComment(line) {
			continue
		}
		for _, pattern := range browserPatterns() {
			if !seen[pattern.Pattern] && strings.Contains(line, pattern.Pattern) {
				seen[pattern.Pattern] = true
				matches = append(matches, browserMatch{pattern, audit.Evidence{Path: src.path, Line: i + 1, Text: clip(line)}})
			}
		}
	}
	return matches
}
