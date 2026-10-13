package rails

import (
	"os"
	"sort"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/detect"
)

type agentPattern struct {
	Pattern string
	Vendor  string
}

// agentPatterns is the table of section 4.7 of the plan.
func agentPatterns() []agentPattern {
	return []agentPattern{
		{"datadog/agent", "Datadog Agent"},
		{"datadog-agent", "Datadog Agent"},
		{"dd-agent", "Datadog Agent"},
		{"datadog.yaml", "Datadog Agent"},
		{"heroku-buildpack-datadog", "Datadog Agent"},
		{"DD_AGENT_HOST", "Datadog Agent"},
		{"DD_API_KEY", "Datadog Agent"},
		{"newrelic-infra", "New Relic Infrastructure"},
		{"newrelic/infrastructure", "New Relic Infrastructure"},
		{"otel/opentelemetry-collector", "OpenTelemetry Collector"},
		{"otelcol", "OpenTelemetry Collector"},
		{"otel-collector-config", "OpenTelemetry Collector"},
		{"grafana/alloy", "Grafana Alloy"},
		{"grafana/agent", "Grafana Alloy"},
		{"timberio/vector", "Vector"},
		{"vector.toml", "Vector"},
		{"vector.yaml", "Vector"},
		{"fluent/fluent-bit", "Fluent Bit"},
		{"fluent-bit.conf", "Fluent Bit"},
	}
}

// herokuPrerun is the script that the Datadog buildpack for Heroku runs before it starts the
// Agent. The buildpack itself is set on the Heroku app, not in the repository; Forem has only
// this file.
const herokuPrerun = "datadog/prerun.sh"

func agentFiles(root *os.Root) []string {
	files := list(root, ".", func(base string) bool {
		return (NamesOnly(base) && !isEnvFile(base)) || base == ".buildpacks" || base == "heroku.yml"
	})
	files = append(files, list(root, "config", func(base string) bool { return base == "deploy.yml" })...)
	files = append(files, list(root, ".github/workflows", isYAML)...)
	files = append(files, list(root, "bin", anyName)...)
	files = append(files, list(root, "datadog", anyName)...)
	for _, dir := range []string{"k8s", "kubernetes", "deploy", "helm", "charts"} {
		files = append(files, walk(root, dir, anyName)...)
	}
	return files
}

// agents finds an agent or collector that the repository starts or configures. A file that only
// runs a script (`Procfile` runs `bin/with-datadog`) is evidence when that script is.
func agents(root *os.Root) []detect.Agent {
	sources := readSources(root, agentFiles(root))
	found := map[string][]detect.Evidence{}
	scripts := map[string]string{}
	for _, src := range sources {
		for _, vendor := range agentVendors() {
			if evidence, ok := firstAgentLine(src, vendor); ok {
				found[vendor] = append(found[vendor], evidence)
				if strings.HasPrefix(src.path, "bin/") {
					scripts[src.path] = vendor
				}
			}
		}
	}
	if _, ok := readFile(root, herokuPrerun); ok {
		found[vendorDatadogAgent] = append(found[vendorDatadogAgent], detect.Evidence{Path: herokuPrerun})
	}
	for _, src := range sources {
		for script, vendor := range scripts {
			if evidence, ok := firstLineWith(src, script); ok && src.path != script && !hasPath(found[vendor], src.path) {
				found[vendor] = append(found[vendor], evidence)
			}
		}
	}
	result := []detect.Agent{}
	for vendor, evidence := range found {
		sortEvidence(evidence)
		result = append(result, detect.Agent{Vendor: vendor, Evidence: evidence})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Vendor < result[j].Vendor })
	return result
}

func agentVendors() []string {
	var vendors []string
	for _, pattern := range agentPatterns() {
		if len(vendors) == 0 || vendors[len(vendors)-1] != pattern.Vendor {
			vendors = append(vendors, pattern.Vendor)
		}
	}
	return vendors
}

func firstAgentLine(src source, vendor string) (detect.Evidence, bool) {
	for i, line := range src.lines {
		if isComment(line) {
			continue
		}
		for _, pattern := range agentPatterns() {
			if pattern.Vendor == vendor && strings.Contains(line, pattern.Pattern) {
				return agentEvidence(src.path, i+1, pattern.Pattern), true
			}
		}
	}
	return detect.Evidence{}, false
}

func firstLineWith(src source, text string) (detect.Evidence, bool) {
	for i, line := range src.lines {
		if !isComment(line) && strings.Contains(line, text) {
			return agentEvidence(src.path, i+1, text), true
		}
	}
	return detect.Evidence{}, false
}

// agentEvidence never carries the line. A deploy file or a script can hold a secret on the line
// that names the agent, so the text is the pattern that matched, and nothing for a file that is
// read for names only.
func agentEvidence(file string, line int, pattern string) detect.Evidence {
	if NamesOnly(file) || mentionsTrackedName(pattern) {
		pattern = ""
	}
	return detect.Evidence{Path: file, Line: line, Text: pattern}
}

func hasPath(evidence []detect.Evidence, file string) bool {
	for _, e := range evidence {
		if e.Path == file {
			return true
		}
	}
	return false
}
