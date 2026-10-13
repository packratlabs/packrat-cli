package rails

import (
	"os"
	"path"
	"sort"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/detect"
)

func envPrefixes() []string {
	return []string{
		"DD_", "DATADOG_", "SENTRY_", "NEW_RELIC_", "OTEL_", "HONEYBADGER_", "BUGSNAG_", "ROLLBAR_",
		"SCOUT_", "APPSIGNAL_", "ELASTIC_APM_", "POSTHOG_", "PACKRAT_",
	}
}

// NamesOnly reports whether a file is read for the names of environment variables. Such a file
// can hold a secret on any line, so no line of it ever becomes evidence text.
func NamesOnly(name string) bool {
	dir, base := path.Split(name)
	switch dir {
	case "":
		return isEnvFile(base) || base == "app.json" ||
			base == "fly.toml" || base == "Procfile" || strings.HasPrefix(base, "Dockerfile") || isCompose(base)
	case "config/":
		return base == "deploy.yml"
	case ".github/workflows/":
		return isYAML(base)
	}
	return false
}

// isEnvFile also knows `.env_sample` and `.env-example`; Forem keeps its names in `.env_sample`.
func isEnvFile(base string) bool {
	return base == ".env" || strings.HasPrefix(base, ".env.") ||
		strings.HasPrefix(base, ".env_") || strings.HasPrefix(base, ".env-")
}

func isCompose(base string) bool {
	return isYAML(base) && (strings.HasPrefix(base, "docker-compose") || strings.HasPrefix(base, "compose"))
}

func namesOnlyFiles(root *os.Root) []string {
	files := list(root, ".", NamesOnly)
	files = append(files, list(root, "config", func(base string) bool { return base == "deploy.yml" })...)
	return append(files, list(root, ".github/workflows", isYAML)...)
}

func envNames(root *os.Root) []detect.EnvName {
	files := map[string][]string{}
	for _, src := range readSources(root, namesOnlyFiles(root)) {
		seen := map[string]bool{}
		inValue := false
		for _, line := range src.lines {
			if wasInValue := inValue; isEnvFile(src.path) && oddQuotes(line) {
				inValue = !inValue
				if wasInValue {
					continue
				}
			} else if inValue {
				continue
			}
			for _, name := range namesOf(src.path, line) {
				if !seen[name] {
					seen[name] = true
					files[name] = append(files[name], src.path)
				}
			}
		}
	}
	names := []detect.EnvName{}
	for name, where := range files {
		sort.Strings(where)
		names = append(names, detect.EnvName{Name: name, Files: where})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })
	return names
}

// oddQuotes finds the first and last line of a quoted value that spans lines, such as a PEM key
// in a .env file. The lines between are a value and are skipped whole.
func oddQuotes(line string) bool {
	return strings.Count(line, `"`)%2 == 1
}

func namesOf(file, line string) []string {
	if isComment(line) {
		return nil
	}
	if file == "Procfile" {
		return procfileNames(line)
	}
	if name := assignedName(line); name != "" {
		return []string{name}
	}
	return nil
}

// assignedName keeps what is left of the first `=` or `:` and never looks at what is right of
// it. What is left must then begin with the name: `NAME`, `- NAME`, `export NAME`, `ENV NAME`,
// `"NAME"`. A word after the name is not read either, so the value of `ENV NAME value` is safe.
// A line with no `=` or `:` gives a name only as a list item or after ENV; any other such line
// could be a bare value.
func assignedName(line string) string {
	left := strings.TrimSpace(line)
	cut := strings.IndexAny(left, "=:")
	if cut >= 0 {
		left = strings.TrimSpace(left[:cut])
	}
	bare := cut < 0
	for _, lead := range []string{"- ", "ENV ", "ARG ", "export "} {
		if rest, found := strings.CutPrefix(left, lead); found {
			left = strings.TrimSpace(rest)
			bare = bare && lead == "export "
		}
	}
	if bare {
		return ""
	}
	left = strings.TrimLeft(left, `"'`)
	end := 0
	for end < len(left) && isWordByte(left[end]) {
		end++
	}
	if end < len(left) && !strings.ContainsRune(" \t\"'", rune(left[end])) {
		return ""
	}
	return tracked(left[:end])
}

// procfileNames reads `web: NAME=value command`. Only a word with a `=` can give a name, and
// only its left side is kept.
func procfileNames(line string) []string {
	var names []string
	for _, word := range strings.Fields(line) {
		if cut := strings.IndexByte(word, '='); cut > 0 {
			if name := tracked(word[:cut]); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// tracked returns name when it is an upper-case variable name with a prefix the detection keeps.
func tracked(name string) string {
	for i := 0; i < len(name); i++ {
		if c := name[i]; c != '_' && (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return ""
		}
	}
	for _, prefix := range envPrefixes() {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return name
		}
	}
	return ""
}

func mentionsTrackedName(line string) bool {
	for _, prefix := range envPrefixes() {
		if strings.Contains(line, prefix) {
			return true
		}
	}
	return false
}
