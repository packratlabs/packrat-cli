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
		"auth":  {summary: "manage your sign-in to Packrat"},
		"setup": {summary: "set up Packrat in a project"},
		"check": {summary: "manage PR verifications"},
	}
}

func commands() map[string]command {
	return map[string]command{
		"apps": {
			summary: "list your apps",
			usage:   "packrat apps [--json]",
			flags:   [][2]string{{"--json", "print the answer of the API"}},
			description: "Each app is a service and an environment that has sent data, with the time of its last data.\n" +
				"An app you do not know is usually yours under another name.",
			examples: []string{"packrat apps"},
		},
		"auth status": {
			summary:     "show which org and which key the commands use",
			usage:       "packrat auth status",
			description: "PACKRAT_API_KEY is used when it is set, and otherwise the key of packrat login.",
			examples:    []string{"packrat auth status"},
		},
		"setup detect": {
			summary: "scan a project to determine existing observability tools",
			usage:   "packrat setup detect [PATH] [-v] [--json] [--host]",
			args:    [][2]string{{"PATH", "the directory to read (default: the current directory)"}},
			flags: [][2]string{
				{"-v, --verbose", "also print where each thing was found"},
				{"--json", "print the JSON document, which always holds everything"},
				{"--host", "also probe this machine for a Datadog Agent; it opens one local socket"},
			},
			description: "It reads files only. It writes no file and sends nothing. Ruby on Rails is the only stack yet.",
			examples:    []string{"packrat setup detect", "packrat setup detect ~/projects/shop --verbose"},
		},
		"login": {
			summary: "sign in to Packrat in your browser",
			usage:   "packrat login [--no-browser]",
			flags:   [][2]string{{"--no-browser", "print the address and the code instead of opening the browser"}},
			description: "The key it makes is yours, for this machine. It is kept in the macOS Keychain, or elsewhere in\n" +
				"~/.config/packrat/credentials.json. The org's keys page lists it, and it can be revoked there.",
			examples: []string{"packrat login"},
		},
		"logout": {
			summary:     "sign out and revoke this machine's key",
			usage:       "packrat logout",
			description: "It does not touch PACKRAT_API_KEY.",
			examples:    []string{"packrat logout"},
		},
		"open": {
			summary: "read a packrat.sh URL",
			usage:   "packrat open URL",
			args:    [][2]string{{"URL", "a Packrat address, or a whole pasted line that holds one"}},
			description: "It prints markdown where the page has it, and otherwise the page's HTML. The key is sent only to\n" +
				"packrat.sh and its subdomains. A GitHub pull request's URL is the same as packrat pr.",
			examples: []string{
				"packrat open https://query.packrat.sh/reports/rep_5k2m9x0qv7ha",
				"packrat open 'curl -sS https://query.packrat.sh/reports/rep_5k2m9x0qv7ha -H …'",
			},
		},
		"setup plan": {
			summary: "scan a project and suggest observability tools",
			usage:   "packrat setup plan [PATH] [--app SERVICE/ENV] [-v] [--json]",
			args:    [][2]string{{"PATH", "the directory to read (default: the current directory)"}},
			flags: [][2]string{
				{"--app=<value>", "leave out the signals that Packrat already receives for this app"},
				{"-v, --verbose", "also print the reason for each step"},
				{"--json", "print the JSON document"},
			},
			description: "It sends nothing, except with --app, when it asks query.packrat.sh.",
			examples:    []string{"packrat setup plan", "packrat setup plan --app shop/production"},
		},
		"pr": {
			summary: "show how a pull request went in production",
			usage:   "packrat pr NUMBER|URL [--app SERVICE] [--json]\n  $ packrat pr NUMBER|URL check --due TIME [--app SERVICE] < check.txt",
			args:    [][2]string{{"NUMBER|URL", "the pull request: its number, or its GitHub URL"}},
			flags: [][2]string{
				{"--app=<value>", "the app (default: the one connected to the pull request's repository)"},
				{"--due=<value>", "with check: the first time it can be verified, in UTC"},
				{"--json", "print the answer of the API"},
			},
			description: "Each deploy that shipped it, the analyst's verdict and evidence, and its checks. With a number,\n" +
				"the repository is this checkout's origin remote.\n\n" +
				"check asks the analyst to verify something for it later. The text on stdin says what must be true\n" +
				"and the SQL that shows it. The answer is mailed in the thread of the pull request's verdict.",
			examples: []string{"packrat pr 482", "packrat pr https://github.com/acme/shop/pull/482", "packrat pr 482 check --due 2026-10-20T21:00Z < check.txt"},
		},
		"sql": {
			summary: "query telemetry data",
			usage:   "packrat sql [SQL] [--app SERVICE] [-f FILE] [--quiet]",
			args:    [][2]string{{"SQL", "the script (default: -f, else stdin)"}},
			flags: [][2]string{
				{"--app=<value>", "the app, when the org has more than one"},
				{"-f=<value>", "read the script from a file"},
				{"--quiet", "leave out the summary line on stderr"},
			},
			description: "It prints NDJSON rows on stdout, and a line on stderr that names the app that answered.",
			examples:    []string{`packrat sql "select count(*) from packrat_spans(since := current_date)"`, "packrat sql --app shop -f question.sql"},
		},
		"status": {
			summary: "verify Packrat is receiving data",
			usage:   "packrat status --app SERVICE/ENV [--wait] [--json]",
			flags: [][2]string{
				{"--app=<value>", "the app, as packrat apps prints it"},
				{"--wait", "wait until new data arrives, for 5 minutes at most"},
				{"--json", "print the answer of the API"},
			},
			description: "Each signal, each source that sends it, and whether Packrat keeps it.",
			examples:    []string{"packrat status --app shop/production"},
		},
		"check add": {
			summary: "ask Packrat to verify something at a later time",
			usage:   "packrat check add --due TIME [--app SERVICE] [--pull NUMBER] [--report ID] < check.txt",
			flags: [][2]string{
				{"--due=<value>", "the first time it can be verified, in UTC, at most 30 days ahead"},
				{"--app=<value>", "the app, when the org has more than one"},
				{"--pull=<value>", "the pull request it is for"},
				{"--report=<value>", "the analyst's mail whose thread the answer joins"},
			},
			description: "The text on stdin says what must be true and the SQL that shows it.",
			examples:    []string{"packrat check add --due 2026-10-20T21:00Z --report rep_5k2m9x0qv7ha < check.txt"},
		},
		"check close": {
			summary:  "cancel a check before Packrat verifies it",
			usage:    "packrat check close ID",
			args:     [][2]string{{"ID", "the check, as chk_…"}},
			examples: []string{"packrat check close chk_7ha5k2m9x0qv"},
		},
		"check show": {
			summary:     "show detailed check information",
			usage:       "packrat check show ID [--app SERVICE]",
			args:        [][2]string{{"ID", "the check, as packrat check list prints it"}},
			flags:       [][2]string{{"--app=<value>", "the app, when the org has more than one"}},
			description: "What was asked, and what the data showed once it closed. It finds open checks, and the ones\nthat closed in the last week.",
			examples:    []string{"packrat check show chk_7ha5k2m9x0qv --app shop"},
		},
		"check list": {
			summary: "list the checks",
			usage:   "packrat check list [--app SERVICE] [--pull NUMBER] [--json]",
			flags: [][2]string{
				{"--app=<value>", "the app, when the org has more than one"},
				{"--pull=<value>", "only the checks of this pull request"},
				{"--json", "print the answer of the API"},
			},
			description: "One line for each: the pull request, when it is due or when it closed (UTC), and the start of\nwhat was asked. Open checks come first. packrat check show prints the whole text.",
			examples:    []string{"packrat check list", "packrat check list --pull 482"},
		},
	}
}

// helpAsked says whether args ask for help, and for what: `packrat help pr`, `packrat pr --help`.
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
