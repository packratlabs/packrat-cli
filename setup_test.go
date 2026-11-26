package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

const (
	setupUnread      = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":false,"accounts":[],"install_url":"/github/setup?state=s1"},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupWrong       = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":false,"accounts":["dana"],"install_url":"/github/setup?state=s1"},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupNoApp       = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":false,"readable":false,"accounts":[],"install_url":null},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupUnconnected = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"account":"acme","accounts":["acme"]},"connected":true,"unconnected":[{"service":"shop","envs":["development","production"],"last_data_at":"2026-11-14T17:58:00Z"},{"service":"blog","envs":["production"],"last_data_at":null}],"apps":[],"setup_url":"/s","now":"2026-11-14T18:00:00Z"}`
	setupAdopted     = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,"unconnected":[],` + setupApps + `,"setup_url":"/s"}`
	setupReadable    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"account":"acme","accounts":["acme"],"install_url":"/github/setup?state=s1"},"connected":true,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupApps        = `"apps":[{"app":"shop/development","env":"development","analysed":false,"last_data_at":"2026-11-14T16:00:00Z"}]`
	setupDeclared    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"pull":{"number":1,"declared":["Traces","Logs"],"checks":[]},"setup_url":"/s"}`
	setupWaived      = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"pull":{"number":1,"declared":[],"none":"It only adds the gem.","checks":[]},"setup_url":"/s"}`
	setupProd        = `"production":{"app":"shop/production","last_data_at":"2026-11-14T17:00:00Z","version":"0000000aaaabbbb"}`
	setupOtherShip   = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,"pull":{"number":1,"declared":["Traces"],"checks":[]},"setup_url":"/s"}`
	setupNoVersion   = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"production":{"app":"shop/production","last_data_at":"2026-11-14T17:00:00Z","version":null},"pull":{"number":1,"declared":["Traces"],"checks":[]},"setup_url":"/s"}`
	setupShipped     = `"pull":{"number":1,"title":"Set up Packrat","state":"pending","declared":[],"version":"15655c04acf7","live_at":"2026-11-14T17:05:00Z",`
	setupRunning     = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","due_at":"2026-11-14T18:05:00Z"}]},"setup_url":"/s"}`
	setupFailed      = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","result":"failed","due_at":"2026-11-14T18:05:00Z"}]},"setup_url":"/s"}`
	setupComplete    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","result":"passed","due_at":"2026-11-14T18:05:00Z"}],"comment_url":"https://github.com/acme/shop/pull/1#issuecomment-1"},"setup_url":"/s"}`
	pluginListed     = "Installed plugins:\n\n  ❯ packrat@packrat\n    Version: 0.4.1\n    Status: ✔ enabled\n"
	pluginOff        = "Installed plugins:\n\n  ❯ packrat@packrat\n    Version: 0.4.1\n    Status: ✘ disabled\n\n  ❯ other@packrat\n    Status: ✔ enabled\n"
	railsLock        = "GEM\n  remote: https://rubygems.org/\n  specs:\n    rails (8.1.0)\n"
	packratLock      = railsLock + "    packrat-rails (0.3.1)\n"
)

// setupCase is one run of packrat setup: what the API answers, what is on the machine, and what the person types.
type setupCase struct {
	name string
	args []string
	// states are the answers of GET /setup/state in turn, the last one repeating; connect is POST /setup/repository's,
	// or the last state when empty.
	states      []string
	stateCode   int
	connect     string
	connectCode int
	signedOut   bool
	origin      string
	files       map[string]string
	// plugins is what claude plugin list prints; noClaude means the program is missing, listFails that it fails, and
	// installFails that install does.
	plugins      string
	noClaude     bool
	listFails    bool
	installFails bool
	terminal     bool
	width        int
	// env is set beside the API's addresses, such as the AI_AGENT a coding agent runs the CLI with.
	env         map[string]string
	stdin       string
	browseFails bool
	// noGit, noCommits and noGh are a directory outside git, a repository with no commit, and no gh on the PATH.
	noGit, noCommits, noGh bool

	code     int
	stdout   []string
	stderr   string
	absent   []string
	browsed  string
	ran      []string
	replaced string
	asked    []string
	unasked  []string
	// reported are lines each of the run's reports to POST /setup/events must hold, the last report first.
	reported []string
}

func TestSetup(t *testing.T) {
	rails := map[string]string{"Gemfile.lock": railsLock}
	withGem := map[string]string{"Gemfile.lock": packratLock, "config/initializers/packrat.rb": "Packrat.configure"}
	gemOnly := map[string]string{"Gemfile.lock": packratLock}
	tests := []setupCase{
		{name: "piped, with GitHub not approved: it prints the page, says to run setup again, and ends without waiting",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Signed in       dana@example.com", "✓ Repository      acme/shop (from origin)", "✓ Platform        Ruby on Rails 8.1.0 (from Gemfile.lock)",
				"◷ GitHub          Packrat requires read-only code access and permission to comment on pull requests.",
				"Open this page and approve Packrat for acme/shop:\n                  SITE/github/setup?state=s1",
				"Next: approve Packrat on GitHub, then run packrat setup again."},
			absent: []string{"Press Enter", "Waiting for you", "✓ Plugin", "\x1b["}, asked: []string{"GET /setup/state?repo=acme%2Fshop"}},
		{name: "a run that stops at GitHub shows every row that is left, not reached",
			states: []string{setupUnread}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"◷ GitHub          Packrat requires read-only code access and permission to comment on pull requests.\n" +
				"○ Plugin          Claude Code Packrat Plugin\n○ Data            Receiving telemetry data from the app\n" +
				"○ Pull request    Opened by Claude Code\n○ PR Verification Packrat runs its first PR verification\n"}},
		{name: "a signed-out run shows all ten rows before it asks for anything",
			signedOut: true, states: []string{setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\nn\n", code: 0,
			stdout: []string{"◷ Signed in       Not yet. Signing you in.\n○ Repository      The GitHub repository of this checkout\n○ Platform        The app's framework\n" +
				"○ GitHub          You approve Packrat for the repository\n", "○ PR Verification Packrat runs its first PR verification\n\nYour sign-in code is"},
			browsed: "SITE/cli/activate?code=BCDF-GHJK"},
		{name: "piped, with GitHub approved: every row, and the command for Claude Code",
			states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ GitHub          Packrat can read acme/shop", "○ Data            None yet. Install packrat-rails", "✓ Plugin          packrat@packrat is installed in Claude Code",
				"○ Pull request    Opened by Claude Code", "○ PR Verification Packrat runs its first PR verification",
				"Next: have Claude Code create a pull request to install Packrat.",
				"  claude 'Set up Packrat in this app. Run `packrat open \"SITE/orgs/org_a/apps/setup?framework=rails\"` and follow its steps.'"},
			absent: []string{"[Y/n]", "\x1b["}, asked: []string{"POST /setup/repository?repo=acme%2Fshop"},
			reported: []string{`[{"step":"Signed in","mark":"done"},{"step":"Repository","mark":"done","text":"acme/shop (from origin)"},` +
				`{"step":"Platform","mark":"done","text":"Ruby on Rails 8.1.0 (from Gemfile.lock)"}`}},
		{name: "a Rails older than 7.1 stops at the platform and connects nothing",
			states: []string{setupReadable}, files: map[string]string{"Gemfile.lock": strings.Replace(railsLock, "rails (8.1.0)", "rails (6.1.7.10)", 1)}, code: 1,
			stdout: []string{"✗ Platform        Ruby on Rails 6.1.7.10 (from Gemfile.lock)\n                  Packrat requires Rails 7.1 or later.\n○ GitHub          You approve Packrat for the repository\n",
				"Next: upgrade the app to Rails 7.1 or later, then run packrat setup again."},
			unasked:  []string{"/setup/state", "/setup/repository"},
			reported: []string{`{"step":"Platform","mark":"wrong","text":"Ruby on Rails 6.1.7.10 (from Gemfile.lock) Packrat requires Rails 7.1 or later."}`}},
		{name: "Rails 7.1 goes on to GitHub",
			states: []string{setupReadable}, files: map[string]string{"Gemfile.lock": strings.Replace(railsLock, "rails (8.1.0)", "rails (7.1.6)", 1)}, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Platform        Ruby on Rails 7.1.6 (from Gemfile.lock)", "✓ GitHub          Packrat can read acme/shop"}},
		{name: "run by Claude Code, it names the page for Claude Code to open rather than starting another",
			states: []string{setupReadable}, files: rails, plugins: pluginListed, env: map[string]string{"AI_AGENT": "claude-code_2-1-331_agent"}, code: 0,
			stdout: []string{"Next: have Claude Code create a pull request to install Packrat.",
				"  Run `packrat open \"SITE/orgs/org_a/apps/setup?framework=rails\"` and follow its steps."},
			absent: []string{"claude 'Set up", "[Y/n]"}},
		{name: "on a terminal of known width the GitHub row is drawn again in place",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, width: 120, stdin: "\nn\n", code: 0,
			stdout: []string{"\x1b[9A\r\x1b[J✓ GitHub          Packrat can read acme/shop"}, browsed: "SITE/github/setup?state=s1"},
		{name: "a narrow terminal counts the lines a row wraps to",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, width: 80, stdin: "\nn\n", code: 0,
			stdout: []string{"\x1b[10A\r\x1b[J✓ GitHub"}, browsed: "SITE/github/setup?state=s1"},
		{name: "at a terminal it waits for Enter, opens GitHub, and starts Claude Code on yes",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\n\n", code: 0,
			stdout:  []string{"Press Enter to open GitHub and approve...", "Waiting for you to approve Packrat for acme/shop on GitHub", "Start Claude Code to do that now? [Y/n]"},
			absent:  []string{"Open this page"},
			browsed: "SITE/github/setup?state=s1", replaced: "claude Set up Packrat in this app.",
			reported: []string{`"phase":"exit"`, `"phase":"claude"`, `{"step":"Plugin","mark":"done","text":"packrat@packrat is installed in Claude Code"},{"step":"Data","mark":"todo"`, `{"step":"GitHub","mark":"wait","text":"Packrat requires`}},
		{name: "answering no prints the command and starts nothing",
			states: []string{setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "n\n", code: 0,
			stdout: []string{"[Y/n]", "  claude 'Set up Packrat", "Run packrat setup again at any time"}},
		{name: "end of input at the question starts nothing",
			states: []string{setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "", code: 0,
			stdout: []string{"[Y/n]", "  claude 'Set up Packrat"}},
		{name: "--no-claude prints the command without asking",
			args: []string{"--no-claude"}, states: []string{setupReadable}, files: rails, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"  claude 'Set up Packrat"}, absent: []string{"[Y/n]"}},
		{name: "--no-browser prints the page at a terminal too",
			args: []string{"--no-browser", "--no-claude"}, states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"Open this page and approve Packrat for acme/shop"}, absent: []string{"Press Enter"}},
		{name: "a browser that will not open falls back to the page's address",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\nn\n", browseFails: true, code: 0,
			stdout: []string{"Press Enter", "Open this page and approve Packrat for acme/shop"}},
		{name: "approving the wrong account says which account to choose, one time",
			args: []string{"--no-browser", "--no-claude"}, states: []string{setupUnread, setupWrong, setupWrong, setupReadable}, files: rails, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"You approved Packrat for dana, but it still cannot read acme/shop.\n                  On GitHub, choose acme and include this repository.", "✓ GitHub"}},
		{name: "GitHub never approved gives up after the limit",
			args: []string{"--no-browser"}, states: []string{setupUnread}, files: rails, plugins: pluginListed, terminal: true, code: 1,
			stderr:   "GitHub did not let Packrat read acme/shop within 5m0s. Run packrat setup again once you have approved it.",
			reported: []string{`"exit":1,"error":"packrat setup: GitHub did not let Packrat read acme/shop within 5m0s.`, `"phase":"waiting"`}},
		{name: "a Packrat with no GitHub App stops at GitHub",
			states: []string{setupNoApp}, files: rails, plugins: pluginListed, code: 1,
			stdout: []string{"✗ GitHub          This Packrat has no GitHub App", "○ Plugin          Claude Code Packrat Plugin"}, absent: []string{"✓ Plugin"}},
		{name: "signed out and not at a terminal signs in first",
			signedOut: true, states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"◷ Signed in       Not yet. Signing you in.", "✓ Signed in       dana@example.com"}, absent: []string{"Press Enter"},
			browsed: "SITE/cli/activate?code=BCDF-GHJK"},
		{name: "signed out at a terminal waits for Enter before the browser, then for Enter before GitHub",
			signedOut: true, states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\n\nn\n", code: 0,
			stdout: []string{"Your sign-in code is BCDF-GHJK.\nPress Enter to open Packrat in your browser and sign in...", "✓ Signed in       dana@example.com",
				"Press Enter to open GitHub and approve...", "[Y/n]", "  claude 'Set up Packrat"}, browsed: "SITE/github/setup?state=s1"},
		{name: "no origin says how to put the app on GitHub, and asks nothing past who is signed in",
			origin: "none", states: []string{setupReadable}, files: rails, code: 1,
			reported: []string{`[{"step":"Signed in","mark":"done"},{"step":"Repository","mark":"wrong","text":"This app is not on GitHub yet.`},
			stdout: []string{"✓ Signed in       dana@example.com", "✗ Repository      This app is not on GitHub yet.",
				"\n\n                    gh repo create --private --source . --push\n\n                  Then run packrat setup again.",
				"✓ Platform        Ruby on Rails 8.1.0 (from Gemfile.lock)", "○ GitHub          You approve Packrat for the repository"},
			absent: []string{"git add -A", "git init", "○ Platform"}, asked: []string{"GET /cli/whoami"}},
		{name: "no origin and no commit commits before it creates the repository",
			origin: "none", noCommits: true, states: []string{setupReadable}, files: rails, code: 1,
			stdout: []string{"    git add -A && git commit -m \"First commit\"\n                    gh repo create --private --source . --push"}},
		{name: "no git repository starts with git init",
			origin: "none", noGit: true, states: []string{setupReadable}, code: 1,
			stdout: []string{"✗ Repository      This directory is not a git repository.", "Run packrat setup in your app's directory.",
				"    git init\n                    git add -A && git commit -m \"First commit\"\n                    gh repo create",
				"✗ Platform        Not a Rails app."}},
		{name: "no origin and no gh names GitHub's page and the git commands",
			origin: "none", noGh: true, states: []string{setupReadable}, files: rails, code: 1,
			stdout: []string{"Create a private repository at https://github.com/new, then run:",
				"    git remote add origin <the repository's URL>\n                    git push -u origin HEAD"},
			absent: []string{"gh repo create"}},
		{name: "an origin that is not on GitHub is named, with no commands to run",
			origin: "git@gitlab.com:acme/shop.git", states: []string{setupReadable}, files: rails, code: 1,
			stdout: []string{"✗ Repository      This checkout's origin is git@gitlab.com:acme/shop.git, which is not on GitHub.",
				"Packrat verifies pull requests on GitHub only.", "✓ Platform"},
			absent: []string{"gh repo create", "git remote add"}},
		{name: "an HTTPS origin names the same repository",
			origin: "https://github.com/acme/shop.git", states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Repository      acme/shop"}},
		{name: "not a Rails app stops at the platform and connects nothing",
			states: []string{setupReadable}, plugins: pluginListed, code: 1,
			stdout: []string{"✗ Platform        Not a Rails app.\n                  packrat setup sets up Rails apps. For other stacks, see https://packrat.sh/docs/quickstart\n" +
				"○ GitHub          You approve Packrat for the repository\n",
				"Next: send OpenTelemetry from the app to Packrat: https://packrat.sh/docs/quickstart"},
			unasked: []string{"/setup/state", "/setup/repository"}},
		{name: "Claude Code missing leaves the plugin and prints the command",
			states: []string{setupReadable}, files: rails, noClaude: true, terminal: true, code: 0,
			stdout: []string{"○ Plugin          Claude Code is not on this shell's PATH, so setup cannot install the plugin.\n" +
				"                  Run packrat setup in the shell where you run claude"}, absent: []string{"[Y/n]"},
			reported: []string{`{"step":"Plugin","mark":"todo","text":"Claude Code is not on this shell's PATH`}},
		{name: "a claude that fails to list its plugins says why, rather than that it is missing",
			states: []string{setupReadable}, files: rails, listFails: true, terminal: true, code: 0,
			stdout: []string{"✗ Plugin          claude plugin list failed:\n                  Error: settings.json is not valid JSON"},
			absent: []string{"not on this shell's PATH", "[Y/n]"}},
		{name: "at a terminal it asks before it installs the plugin, and Enter installs it",
			states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", terminal: true, stdin: "\nn\n", code: 0,
			stdout: []string{"◷ Plugin          Connects Claude Code to Packrat.\n\n" +
				"                  Install the Packrat plugin in Claude Code? [Y/n] ", "✓ Plugin          Installed packrat@packrat in Claude Code"},
			ran: []string{"claude plugin list", "claude plugin marketplace add packratlabs/claude-plugin", "claude plugin install packrat@packrat"}},
		{name: "answering no installs nothing, and Claude Code is not offered",
			states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", terminal: true, stdin: "n\n", code: 0,
			stdout: []string{"Install the Packrat plugin in Claude Code? [Y/n]", "○ Plugin          Not installed. Run packrat setup again to install it.", "  claude 'Set up Packrat"},
			absent: []string{"Start Claude Code"}, ran: []string{"claude plugin list"}},
		{name: "with nobody at the keyboard the plugin is not installed, and it says how",
			states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", code: 0,
			stdout: []string{"○ Plugin          Not installed. Run packrat setup --yes to install it."}, absent: []string{"[Y/n]"}, ran: []string{"claude plugin list"}},
		{name: "--yes with nobody at the keyboard installs the plugin",
			args: []string{"--yes"}, states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", code: 0,
			stdout: []string{"✓ Plugin          Installed packrat@packrat in Claude Code", "  claude 'Set up Packrat"}, absent: []string{"[Y/n]", "◷ Plugin"},
			ran: []string{"claude plugin list", "claude plugin marketplace add packratlabs/claude-plugin", "claude plugin install packrat@packrat"}},
		{name: "--yes at a terminal asks nothing: it opens GitHub, installs the plugin and starts Claude Code",
			args: []string{"-y"}, signedOut: true, states: []string{setupUnread, setupReadable}, files: rails, plugins: "No plugins installed.\n", terminal: true, code: 0,
			stdout:  []string{"✓ Signed in       dana@example.com", "✓ GitHub          Packrat can read acme/shop", "✓ Plugin          Installed packrat@packrat in Claude Code"},
			absent:  []string{"Press Enter", "[Y/n]"},
			browsed: "SITE/github/setup?state=s1", replaced: "claude Set up Packrat in this app."},
		{name: "--yes with --no-claude does everything but start Claude Code",
			args: []string{"--yes", "--no-claude"}, states: []string{setupReadable}, files: rails, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"  claude 'Set up Packrat"}, absent: []string{"[Y/n]"}},
		{name: "a disabled plugin is not counted as installed",
			args: []string{"--yes"}, states: []string{setupReadable}, files: rails, plugins: pluginOff, code: 0,
			stdout: []string{"✓ Plugin          Installed packrat@packrat"}},
		{name: "a plugin install that fails says what claude said, and Claude Code is not offered",
			args: []string{"--yes"}, states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", installFails: true, terminal: true, code: 0,
			stdout: []string{"✗ Plugin          claude plugin install packrat@packrat failed:\n                  network unreachable"}, absent: []string{"[Y/n]", "Start Claude Code"}},
		{name: "the gem without its initializer is not set up yet",
			states: []string{setupReadable}, files: gemOnly, plugins: pluginListed, code: 0,
			stdout: []string{"Next: have Claude Code create a pull request to install Packrat."}},
		{name: "the gem and its initializer with no pull request yet",
			states: []string{setupReadable}, files: withGem, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"Next: have Claude Code open a pull request with the change that adds packrat-rails."}, absent: []string{"[Y/n]"}},
		{name: "a pull request that declared checks waits for its deploy",
			states: []string{setupDeclared}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Data            Receiving from shop/development", "✓ Pull request    #1, 2 checks declared",
				"◷ PR Verification Waiting for a production deploy that ships #1.\n\nNext: merge #1 and deploy to production."},
			absent: []string{"Deploy  ", "Checks  ", "Comment  "}},
		{name: "a pull request that declared none says why",
			states: []string{setupWaived}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request    #1 declared no checks: It only adds the gem."}},
		{name: "production on another version waits for the one that ships the pull request",
			states: []string{setupOtherShip}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request    #1, 1 check declared", "◷ PR Verification Waiting for a production deploy that ships #1.\n"}},
		{name: "production with no service.version is wrong, and says what to set",
			states: []string{setupNoVersion}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✗ PR Verification shop/production is reporting, but its traces carry no service.version.", "BUILD_COMMIT=$(git rev-parse HEAD)"}},
		{name: "checks running after the deploy",
			states: []string{setupRunning}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request    #1 Set up Packrat, 0 checks declared", "◷ PR Verification 1 of 2 checks passed. See packrat pr show 1\n\n",
				"Next: Packrat is running the checks of #1."}, absent: []string{"Traces", "Logs"}},
		{name: "a failed check is wrong and setup is not complete",
			states: []string{setupFailed}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✗ PR Verification 1 of 2 checks passed. See packrat pr show 1\n\n"}, absent: []string{"Setup is complete"}},
		{name: "every check passed: setup is complete",
			states: []string{setupComplete}, files: withGem, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"✓ PR Verification 2 of 2 checks passed\n\nSetup is complete. Packrat verifies every pull request Claude Code opens in acme/shop.\n" +
				"In Claude Code, type /packrat to see how each one did in production.\n"},
			absent: []string{"[Y/n]", "issuecomment"}},
		{name: "the API refusing the state is said and fails",
			states:    []string{`{"type":"error","message":"you belong to more than one org, and none of them reads acme/shop yet. Name the one to set up with org= (in the CLI, --org): org_a (Acme) and org_b (Bolt)."}`},
			stateCode: 400, files: rails, code: 1, stderr: "Name the one to set up with org= (in the CLI, --org)"},
		{name: "connecting refused is said and fails",
			states: []string{setupReadable}, connect: `{"type":"error","message":"GitHub does not let Packrat read acme/shop yet."}`, connectCode: 409, files: rails, code: 1,
			stderr: "connecting acme/shop"},
		{name: "an app that reported before setup ran is asked about, and the one picked is connected",
			states: []string{setupUnconnected}, connect: setupAdopted, files: withGem, plugins: pluginListed, terminal: true, stdin: "x\n1\nn\n", code: 0,
			stdout: []string{"Packrat already receives data from apps that have no repository:\n\n" +
				"                    1  shop  development, production · last data 2 minutes ago\n" +
				"                    2  blog  production\n" +
				"                    3  Neither: this is a new app\n",
				"Which app is acme/shop? [1-3] ", "✓ App             shop is connected to acme/shop", "✓ Data            Receiving from shop/development"},
			asked: []string{"POST /setup/repository?repo=acme%2Fshop&service=shop"}},
		{name: "a new app connects as before, to the org's next app",
			states: []string{setupUnconnected}, files: rails, plugins: pluginListed, terminal: true, stdin: "3\nn\n", code: 0,
			stdout: []string{"Which app is acme/shop? [1-3]"}, absent: []string{"✓ App"},
			asked: []string{"POST /setup/repository?repo=acme%2Fshop\n"}},
		{name: "with nobody to ask, the apps with no repository are named and nothing is picked",
			states: []string{setupUnconnected}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"○ App             Packrat has apps with no repository: shop, blog.\n" +
				"                  Run packrat setup in a terminal to pick one, or connect it on the app's Settings page."},
			absent: []string{"Which app", "service="}},
		{name: "an extra word is a usage error", args: []string{"now"}, states: []string{setupReadable}, code: 2, stderr: "the options are --yes, --no-browser and --no-claude"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { runSetupCase(t, tc) })
	}
}

func runSetupCase(t *testing.T, tc setupCase) {
	t.Helper()
	var asked, reports, runs []string
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") == "Bearer "+loginKey {
			runs = append(runs, r.Header.Get("Packrat-Setup-Run"))
		}
		answer := func(code int, text string) {
			if code == 0 {
				code = 200
			}
			w.WriteHeader(code)
			_, _ = io.WriteString(w, text) // #nosec G705 -- a test's own answer
		}
		switch {
		case r.URL.Path == "/cli/sign_ins":
			answer(201, `{"device_code":"dev","user_code":"BCDF-GHJK","verification_uri":"`+r.Host+`","verification_uri_complete":"http://`+r.Host+`/cli/activate?code=BCDF-GHJK","expires_in":600,"interval":5}`)
		case r.URL.Path == "/cli/token":
			answer(200, `{"access_token":"`+loginKey+`","key":"key_1","email":"dana@example.com"}`)
		case r.Header.Get("Authorization") != "Bearer "+loginKey:
			answer(401, "")
		case r.URL.Path == "/cli/whoami":
			answer(200, `{"key":"key_1","name":"CLI: laptop","source":"cli","email":"dana@example.com","orgs":[{"org":"org_a","name":"Acme"}]}`)
		case r.URL.Path == "/setup/state":
			state := tc.states[min(polls, len(tc.states)-1)]
			polls++
			answer(tc.stateCode, state)
		case r.URL.Path == "/setup/events" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			reports = append([]string{string(body)}, reports...)
			answer(204, "")
		case r.URL.Path == "/setup/repository" && r.Method == http.MethodPost:
			if tc.connect != "" {
				answer(tc.connectCode, tc.connect)
				return
			}
			answer(200, tc.states[len(tc.states)-1])
		default:
			answer(404, "")
		}
	}))
	defer server.Close()

	// Nothing answers at the API's address: setup asks the site alone.
	env := map[string]string{"PACKRAT_QUERY_URL": "http://127.0.0.1:1", "PACKRAT_URL": server.URL}
	for name, value := range tc.env {
		env[name] = value
	}
	store := auth.Store{Dir: t.TempDir()}
	if !tc.signedOut {
		if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
			t.Fatalf("saving the sign-in: %v", err)
		}
	}
	origin := tc.origin
	switch origin {
	case "":
		origin = "git@github.com:acme/shop.git\n"
	case "none":
		origin = ""
	}
	var browsed, replaced string
	var replacedEnv []string
	var ran []string
	w := world{getenv: func(name string) string { return env[name] }, sleep: func(time.Duration) {}, store: store,
		stdin: strings.NewReader(tc.stdin), http: &http.Client{}, hostname: func() (string, error) { return "laptop", nil },
		origin: func() string { return origin },
		width:  func() int { return tc.width },
		browse: func(address string) error {
			if tc.browseFails {
				return errors.New("no browser")
			}
			browsed = address
			return nil
		},
		program: func(name string, args ...string) (string, error) {
			command := name + " " + strings.Join(args, " ")
			ran = append(ran, command)
			switch {
			case tc.noGit && strings.HasPrefix(command, "git rev-parse"),
				tc.noCommits && command == "git rev-parse --verify HEAD",
				tc.noGh && command == "gh --version":
				return "", errors.New("exit 128")
			case strings.HasPrefix(command, "git ") || strings.HasPrefix(command, "gh "):
				return "", nil
			case tc.noClaude:
				return "", fmt.Errorf("running claude: %w", exec.ErrNotFound)
			case tc.listFails && command == "claude plugin list":
				return "Error: settings.json is not valid JSON\n", errors.New("exit status 1")
			case command == "claude plugin list":
				return tc.plugins, nil
			case tc.installFails && strings.Contains(command, "plugin install"):
				return "Installing…\nnetwork unreachable\n", errors.New("exit 1")
			}
			return "done", nil
		},
		replace: func(env []string, name string, args ...string) error {
			replaced = name + " " + strings.Join(args, " ")
			replacedEnv = env
			return nil
		},
		readFile: func(name string) ([]byte, error) {
			if text, found := tc.files[name]; found {
				return []byte(text), nil
			}
			return nil, os.ErrNotExist
		},
		terminal: tc.terminal}
	var stdout, stderr bytes.Buffer

	code := runIn(w, append([]string{"setup"}, tc.args...), &stdout, &stderr)

	here := strings.NewReplacer("SITE", server.URL)
	if code != tc.code {
		t.Errorf("exit %d, want %d\n%s%s", code, tc.code, stdout.String(), stderr.String())
	}
	for _, want := range tc.stdout {
		if !strings.Contains(stdout.String(), here.Replace(want)) {
			t.Errorf("stdout lacks %q:\n%s", here.Replace(want), stdout.String())
		}
	}
	for _, not := range tc.absent {
		if strings.Contains(stdout.String(), not) {
			t.Errorf("stdout must not hold %q:\n%s", not, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), tc.stderr) {
		t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), loginKey) {
		t.Errorf("a key was printed:\n%s%s", stdout.String(), stderr.String())
	}
	if browsed != here.Replace(tc.browsed) {
		t.Errorf("browsed %q, want %q", browsed, here.Replace(tc.browsed))
	}
	if !strings.HasPrefix(replaced, tc.replaced) || (tc.replaced == "") != (replaced == "") {
		t.Errorf("became %q, want %q", replaced, tc.replaced)
	}
	if tc.ran != nil && strings.Join(ran, "\n") != strings.Join(tc.ran, "\n") {
		t.Errorf("ran %q, want %q", ran, tc.ran)
	}
	for i, want := range tc.reported {
		if i >= len(reports) || !strings.Contains(reports[i], want) {
			t.Errorf("report %d lacks %q:\n%s", i, want, strings.Join(reports, "\n"))
		}
	}
	for _, run := range runs {
		if run == "" || run != runs[0] {
			t.Errorf("a request named the run %q, want every one to name the same run: %q", run, runs)
			break
		}
	}
	if replaced != "" && (len(replacedEnv) != 1 || replacedEnv[0] != "PACKRAT_SETUP_RUN="+runs[0]) {
		t.Errorf("Claude Code was started with %q, want the run %q", replacedEnv, runs[0])
	}
	for _, want := range tc.asked {
		if !strings.Contains(strings.Join(asked, "\n")+"\n", want) {
			t.Errorf("did not ask %q:\n%s", want, strings.Join(asked, "\n"))
		}
	}
	for _, unwanted := range tc.unasked {
		if strings.Contains(strings.Join(asked, "\n")+"\n", unwanted) {
			t.Errorf("asked %q:\n%s", unwanted, strings.Join(asked, "\n"))
		}
	}
}

func TestOlderThan(t *testing.T) {
	for version, want := range map[string]bool{"6.1.7.10": true, "7.0.10": true, "7.1.0": false, "7.2.4": false, "8.1.4.1": false, "10.0.0": false, "": false, "edge": false} {
		if got := olderThan(version, minimumRails); got != want {
			t.Errorf("olderThan(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestSetupColoursOnlyWhenAsked(t *testing.T) {
	var plain, coloured bytes.Buffer
	setupOut{w: &plain}.row(markDone, "GitHub", "ok")
	setupOut{w: &coloured, colour: true}.row(markWrong, "GitHub", "no")
	if plain.String() != "✓ GitHub          ok\n" || !strings.Contains(coloured.String(), "\x1b[31m✗\x1b[0m") {
		t.Errorf("rows:\n%q\n%q", plain.String(), coloured.String())
	}
}

func TestPluginEnabled(t *testing.T) {
	for listed, want := range map[string]bool{pluginListed: true, pluginOff: false, "": false, "No plugins installed.": false,
		"  ❯ packrat@other\n    Status: ✔ enabled\n":                                   false,
		"  ❯ a@b\n    Status: ✔ enabled\n  ❯ packrat@packrat\n    Status: ✔ enabled\n": true} {
		if got := pluginEnabled(listed); got != want {
			t.Errorf("pluginEnabled(%q) = %v, want %v", listed, got, want)
		}
	}
}

func TestSetupChecksDraft(t *testing.T) {
	dir := t.TempDir()
	git := func(_ string, args ...string) (string, error) {
		if args[0] == "branch" {
			return "packrat-setup\n", nil
		}
		return dir + "\n", nil
	}
	var stdout, stderr bytes.Buffer
	if code := runIn(world{getenv: func(string) string { return "" }, program: git}, []string{"pr", "checks", "setup"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	path := filepath.Join(dir, "packrat", "checks", "packrat-setup.json")
	draft, err := os.ReadFile(path) // #nosec G304 -- the test's own directory
	if err != nil {
		t.Fatalf("no draft: %v", err)
	}
	var checks []map[string]string
	if err := json.Unmarshal(draft, &checks); err != nil || len(checks) != 3 {
		t.Fatalf("the draft is not a list of three checks: %v\n%s", err, draft)
	}
	for _, check := range checks {
		if check["title"] == "" || check["claim"] == "" || check["pass_when"] != "seen >= 1" || !strings.Contains(check["sql"], "getvariable('version')") {
			t.Errorf("check lacks a field, or does not read the deployed version: %v", check)
		}
	}
	if !strings.Contains(stdout.String(), path) {
		t.Errorf("stdout does not name the draft:\n%s", stdout.String())
	}

	outside := func(string, ...string) (string, error) { return "", errors.New("not a git repository") }
	stdout.Reset()
	if code := runIn(world{getenv: func(string) string { return "" }, program: outside}, []string{"pr", "checks", "setup"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("outside a checkout: exit %d, want %d", code, exitUsage)
	}
}
