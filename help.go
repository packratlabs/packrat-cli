package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// version is set when a release is linked (-X main.version=…), which only a package variable can
// take; nothing else writes it. A local build says dev.
var version = "dev"

const tagline = "CLI to interact with Packrat"

// A topic groups commands that share a first word, as `heroku help apps` does.
type topic struct{ summary string }

type command struct {
	summary     string
	usage       string
	args        [][2]string
	flags       [][2]string
	description string
	examples    []string
}

func topics() map[string]topic {
	return map[string]topic{
		"auth":  {summary: "manage your Packrat sign-in"},
		"setup": {summary: "set up Packrat in a project"},
		"check": {summary: "schedule checks that verify a change in production"},
		"pr":    {summary: "see how pull requests performed in production"},
	}
}

func commands() map[string]command {
	return map[string]command{
		"apps": {
			summary: "list your apps",
			usage:   "packrat apps [--json]",
			flags:   [][2]string{{"--json", "output the API response as JSON"}},
			description: "Lists each app that has sent data to Packrat, with the time of its most recent data. An app\n" +
				"is a service name and an environment. If you don't recognize an app, it's usually one of yours\n" +
				"reporting under a different service name or environment.",
			examples: []string{"packrat apps"},
		},
		"auth status": {
			summary:     "show the org and key that commands use",
			usage:       "packrat auth status",
			description: "Commands use PACKRAT_API_KEY if it's set. Otherwise, they use the key from packrat login.",
			examples:    []string{"packrat auth status"},
		},
		"setup detect": {
			summary: "scan a project for existing observability tools",
			usage:   "packrat setup detect [PATH] [-v] [--json] [--host]",
			args:    [][2]string{{"PATH", "directory to scan (default: the current directory)"}},
			flags: [][2]string{
				{"-v, --verbose", "show where each result was found"},
				{"--json", "output the full results as JSON"},
				{"--host", "also check this machine for a Datadog Agent (opens one local socket)"},
			},
			description: "Reads files only. It doesn't write files or make network requests. Supports Ruby on Rails.",
			examples:    []string{"packrat setup detect", "packrat setup detect ~/projects/shop --verbose"},
		},
		"login": {
			summary: "sign in to Packrat in your browser",
			usage:   "packrat login [--no-browser]",
			flags:   [][2]string{{"--no-browser", "print the sign-in URL and code instead of opening a browser"}},
			description: "Creates an API key for you on this machine. The key is stored in the macOS Keychain, or in\n" +
				"~/.config/packrat/credentials.json on other platforms. You can see and revoke it on your org's\n" +
				"API keys page.",
			examples: []string{"packrat login"},
		},
		"logout": {
			summary:     "sign out and revoke this machine's key",
			usage:       "packrat logout",
			description: "Doesn't affect PACKRAT_API_KEY.",
			examples:    []string{"packrat logout"},
		},
		"open": {
			summary: "read a Packrat page",
			usage:   "packrat open URL",
			args:    [][2]string{{"URL", "a Packrat URL, or a pasted line that contains one"}},
			description: "Prints the page as markdown when it has a markdown version, and as HTML otherwise. Your key is\n" +
				"only sent to packrat.sh and its subdomains. A GitHub pull request URL runs packrat pr show.",
			examples: []string{
				"packrat open https://query.packrat.sh/reports/rep_5k2m9x0qv7ha",
				"packrat open 'curl -sS https://query.packrat.sh/reports/rep_5k2m9x0qv7ha -H …'",
			},
		},
		"setup plan": {
			summary: "suggest how to send each signal to Packrat",
			usage:   "packrat setup plan [PATH] [--app SERVICE/ENV] [-v] [--json]",
			args:    [][2]string{{"PATH", "directory to scan (default: the current directory)"}},
			flags: [][2]string{
				{"--app=<value>", "skip signals that Packrat already receives for this app"},
				{"-v, --verbose", "show the reason for each step"},
				{"--json", "output the plan as JSON"},
			},
			description: "Doesn't make network requests, unless you pass --app.",
			examples:    []string{"packrat setup plan", "packrat setup plan --app shop/production"},
		},
		"pr show": {
			summary: "show how a pull request performed in production",
			usage:   "packrat pr show NUMBER|URL [--app SERVICE] [--json]",
			args:    [][2]string{{"NUMBER|URL", "pull request number or GitHub URL"}},
			flags: [][2]string{
				{"--app=<value>", "app to use (default: the app connected to the pull request's repository)"},
				{"--json", "output the API response as JSON"},
			},
			description: "Shows each deploy that included the pull request, with the analyst's verdict, the evidence and\n" +
				"any checks. With a number, the repository is the origin remote of the current directory.",
			examples: []string{"packrat pr show 482", "packrat pr show https://github.com/acme/shop/pull/482"},
		},
		"sql": {
			summary: "query your telemetry with SQL",
			usage:   "packrat sql [SQL] [--app SERVICE] [-f FILE] [--quiet]",
			args:    [][2]string{{"SQL", "query to run (default: read from -f or stdin)"}},
			flags: [][2]string{
				{"--app=<value>", "app to query, if your org has more than one"},
				{"-f=<value>", "read the query from a file"},
				{"--quiet", "don't print the summary line to stderr"},
			},
			description: "Prints rows as NDJSON to stdout, and a summary line with the app that answered to stderr.",
			examples:    []string{`packrat sql "select count(*) from packrat_spans(since := current_date)"`, "packrat sql --app shop -f question.sql"},
		},
		"status": {
			summary: "verify that Packrat is receiving data",
			usage:   "packrat status [--app SERVICE/ENV] [--wait] [--json]",
			flags: [][2]string{
				{"--app=<value>", "app to check, as packrat apps lists it (needed if your org has more than one)"},
				{"--wait", "wait up to 5 minutes for new data to arrive. Without --app, waits for any app"},
				{"--json", "output the API response as JSON"},
			},
			description: "Shows each signal, the sources that send it, and whether Packrat keeps it.",
			examples:    []string{"packrat status --wait", "packrat status --app shop/production"},
		},
		"check add": {
			summary: "schedule a check for Packrat's analyst",
			usage:   "packrat check add --due TIME [--app SERVICE] [--pull NUMBER] [--report ID] < check.txt",
			flags: [][2]string{
				{"--due=<value>", "earliest time the check can pass, in UTC (up to 30 days from now)"},
				{"--app=<value>", "app to check, if your org has more than one"},
				{"--pull=<value>", "pull request the check is for (sets the app from the origin remote)"},
				{"--report=<value>", "analyst report whose email thread gets the result"},
			},
			description: "Reads the check from stdin: what must be true, and the SQL that shows it. The analyst runs the\n" +
				"check after the due time and emails the result.",
			examples: []string{"packrat check add --due 2026-10-20T21:00Z --pull 482 < check.txt", "packrat check add --due 2026-10-20T21:00Z --report rep_5k2m9x0qv7ha < check.txt"},
		},
		"check close": {
			summary:  "cancel a check before it runs",
			usage:    "packrat check close ID",
			args:     [][2]string{{"ID", "check ID (chk_…)"}},
			examples: []string{"packrat check close chk_7ha5k2m9x0qv"},
		},
		"check show": {
			summary:     "show a check's details",
			usage:       "packrat check show ID [--json]",
			args:        [][2]string{{"ID", "check ID, from packrat check list or packrat pr show"}},
			flags:       [][2]string{{"--json", "output the API response as JSON"}},
			description: "Shows what the check asks for, and the result once it has run.",
			examples:    []string{"packrat check show chk_7ha5k2m9x0qv"},
		},
		"check list": {
			summary: "list checks",
			usage:   "packrat check list [--app SERVICE] [--pull NUMBER] [--json]",
			flags: [][2]string{
				{"--app=<value>", "app to list checks for, if your org has more than one"},
				{"--pull=<value>", "only list checks for this pull request"},
				{"--json", "output the API response as JSON"},
			},
			description: "Shows one line per check: the pull request, when it's due or when it closed (in UTC), and the\n" +
				"start of what it asks. Open checks are listed first. Run packrat check show for the full text.",
			examples: []string{"packrat check list", "packrat check list --pull 482"},
		},
	}
}

// helpAsked says whether args ask for help, and for what: `packrat help pr show`, `packrat pr show --help`.
func helpAsked(args []string) ([]string, bool) {
	if len(args) > 0 && args[0] == "help" {
		return args[1:], true
	}
	var words []string
	asked := false
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help" || arg == "-help":
			asked = true
		case !asked && !strings.HasPrefix(arg, "-") && len(words) < 2:
			words = append(words, arg)
		}
	}
	return words, asked
}

// helpFor is the help of what words name: nothing for the CLI, a topic, or a command. The words are
// the arguments before the first flag; the ones after a command's name are its own arguments.
func helpFor(words []string) (string, bool) {
	if len(words) == 0 {
		return topLevelHelp(), true
	}
	if len(words) > 1 {
		if c, found := commands()[words[0]+" "+words[1]]; found {
			return commandHelp(c), true
		}
	}
	if c, found := commands()[words[0]]; found {
		return commandHelp(c), true
	}
	if t, found := topics()[words[0]]; found {
		return topicHelp(words[0], t), true
	}
	return "", false
}

func topLevelHelp() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nVERSION\n  packrat/%s %s-%s\n\nUSAGE\n  $ packrat [COMMAND]\n\n", tagline, version, runtime.GOOS, runtime.GOARCH)
	var topicRows, commandRows [][2]string
	for name, t := range topics() {
		topicRows = append(topicRows, [2]string{name, t.summary})
	}
	for name, c := range commands() {
		if !strings.Contains(name, " ") {
			commandRows = append(commandRows, [2]string{name, c.summary})
		}
	}
	width := max(columnWidth(topicRows), columnWidth(commandRows))
	b.WriteString("TOPICS\n" + tableAt(topicRows, width) + "\nCOMMANDS\n" + tableAt(commandRows, width))
	return b.String()
}

func topicHelp(name string, t topic) string {
	var rows [][2]string
	for full, c := range commands() {
		if strings.HasPrefix(full, name+" ") {
			rows = append(rows, [2]string{full, c.summary})
		}
	}
	return fmt.Sprintf("%s\n\nUSAGE\n  $ packrat %s COMMAND\n\nCOMMANDS\n%s", t.summary, name, table(rows))
}

func commandHelp(c command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nUSAGE\n  $ %s\n", c.summary, c.usage)
	if len(c.args) > 0 {
		b.WriteString("\nARGUMENTS\n" + table(c.args))
	}
	if len(c.flags) > 0 {
		b.WriteString("\nFLAGS\n" + table(c.flags))
	}
	if c.description != "" {
		b.WriteString("\nDESCRIPTION\n")
		for _, line := range strings.Split(c.description, "\n") {
			b.WriteString(strings.TrimRight("  "+line, " ") + "\n")
		}
	}
	if len(c.examples) > 0 {
		b.WriteString("\nEXAMPLES\n")
		for _, example := range c.examples {
			b.WriteString("  $ " + example + "\n")
		}
	}
	return b.String()
}

// table writes rows as two aligned columns, sorted by the first.
func table(rows [][2]string) string { return tableAt(rows, columnWidth(rows)) }

func columnWidth(rows [][2]string) int {
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	return width
}

func tableAt(rows [][2]string, width int) string {
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, row[0], row[1])
	}
	return b.String()
}
