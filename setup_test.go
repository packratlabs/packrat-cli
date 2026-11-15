package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

const (
	setupUnread    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":false,"accounts":[],"install_url":"/github/setup?state=s1"},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupWrong     = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":false,"accounts":["dana"],"install_url":"/github/setup?state=s1"},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupNoApp     = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":false,"readable":false,"accounts":[],"install_url":null},"connected":false,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupReadable  = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"account":"acme","accounts":["acme"],"install_url":"/github/setup?state=s1"},"connected":true,"apps":[],"setup_url":"/orgs/org_a/apps/setup?framework=rails"}`
	setupApps      = `"apps":[{"app":"shop/development","env":"development","analysed":false,"last_data_at":"2026-11-14T16:00:00Z"}]`
	setupDeclared  = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"pull":{"number":1,"declared":["Traces","Logs"],"checks":[]},"setup_url":"/s"}`
	setupWaived    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"pull":{"number":1,"declared":[],"none":"It only adds the gem.","checks":[]},"setup_url":"/s"}`
	setupProd      = `"production":{"app":"shop/production","last_data_at":"2026-11-14T17:00:00Z","version":"0000000aaaabbbb"}`
	setupOtherShip = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,"pull":{"number":1,"declared":["Traces"],"checks":[]},"setup_url":"/s"}`
	setupNoVersion = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,"production":{"app":"shop/production","last_data_at":"2026-11-14T17:00:00Z","version":null},"pull":{"number":1,"declared":["Traces"],"checks":[]},"setup_url":"/s"}`
	setupShipped   = `"pull":{"number":1,"title":"Set up Packrat","state":"pending","declared":[],"version":"15655c04acf7","live_at":"2026-11-14T17:05:00Z",`
	setupRunning   = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","due_at":"2026-11-14T18:05:00Z"}]},"setup_url":"/s"}`
	setupFailed    = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","result":"failed","due_at":"2026-11-14T18:05:00Z"}]},"setup_url":"/s"}`
	setupComplete  = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"accounts":["acme"]},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"},{"id":"chk_2","title":"Logs","result":"passed","due_at":"2026-11-14T18:05:00Z"}],"comment_url":"https://github.com/acme/shop/pull/1#issuecomment-1"},"setup_url":"/s"}`
	setupNoComment = `{"repository":"acme/shop","org":{"id":"org_a","name":"Acme"},"github":{"configured":true,"readable":true,"account":"acme","accounts":["acme"],"settings_url":"https://github.com/apps/packrat-sh/installations/7"},"connected":true,` + setupApps + `,` + setupProd + `,` + setupShipped + `"checks":[{"id":"chk_1","title":"Traces","result":"passed","due_at":"2026-11-14T18:05:00Z"}],"comment_error":"GitHub answered 403: Resource not accessible by integration"},"setup_url":"/s"}`
	pluginListed   = "Installed plugins:\n\n  ❯ packrat@packrat\n    Version: 0.4.1\n    Status: ✔ enabled\n"
	pluginOff      = "Installed plugins:\n\n  ❯ packrat@packrat\n    Version: 0.4.1\n    Status: ✘ disabled\n\n  ❯ other@packrat\n    Status: ✔ enabled\n"
	railsLock      = "GEM\n  remote: https://rubygems.org/\n  specs:\n    rails (8.1.0)\n"
	packratLock    = railsLock + "    packrat-rails (0.3.1)\n"
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
	// plugins is what claude plugin list prints; noClaude means the program is missing, and installFails that install does.
	plugins      string
	noClaude     bool
	installFails bool
	terminal     bool
	stdin        string
	browseFails  bool

	code     int
	stdout   []string
	stderr   string
	absent   []string
	browsed  string
	ran      []string
	replaced string
	asked    []string
}

func TestSetup(t *testing.T) {
	rails := map[string]string{"Gemfile.lock": railsLock}
	withGem := map[string]string{"Gemfile.lock": packratLock, "config/initializers/packrat.rb": "Packrat.configure"}
	gemOnly := map[string]string{"Gemfile.lock": packratLock}
	tests := []setupCase{
		{name: "piped, with GitHub not approved: it prints the page, says to run setup again, and ends without waiting",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Signed in    dana@example.com", "✓ Repository   acme/shop (from origin)", "✓ Platform     Ruby on Rails 8.1.0 (from Gemfile.lock)",
				"◷ GitHub       Packrat requires read-only code access and the ability to comment on pull requests.",
				"Open this page and approve Packrat for acme/shop:\n               SITE/github/setup?state=s1",
				"Next: approve Packrat on GitHub, then run packrat setup again."},
			absent: []string{"Press Enter", "Waiting for you", "Plugin", "\x1b["}, asked: []string{"GET /setup/state?repo=acme%2Fshop"}},
		{name: "piped, with GitHub approved: every row, and the command for Claude Code",
			states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ GitHub       Packrat can read acme/shop", "○ App          None yet in Acme.", "✓ Plugin       packrat@packrat is installed in Claude Code",
				"○ Pull request None has declared checks yet.", "○ Deploy       No production deploy yet.",
				"Next: have Claude Code add Packrat to this app and open the pull request.",
				"  claude 'Set up Packrat in this app. Run `packrat open SITE/orgs/org_a/apps/setup?framework=rails` and follow its steps.'"},
			absent: []string{"[Y/n]", "\x1b["}, asked: []string{"POST /setup/repository?repo=acme%2Fshop"}},
		{name: "at a terminal it waits for Enter, opens GitHub, and starts Claude Code on yes",
			states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\n\n", code: 0,
			stdout:  []string{"Press Enter to open GitHub and approve...", "Waiting for you to approve Packrat for acme/shop on GitHub", "Start Claude Code to do that now? [Y/n]"},
			absent:  []string{"Open this page"},
			browsed: "SITE/github/setup?state=s1", replaced: "claude Set up Packrat in this app."},
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
			stdout: []string{"You approved Packrat for dana, but it still cannot read acme/shop.\n               On GitHub, choose acme and include this repository.", "✓ GitHub"}},
		{name: "GitHub never approved gives up after the limit",
			args: []string{"--no-browser"}, states: []string{setupUnread}, files: rails, plugins: pluginListed, terminal: true, code: 1,
			stderr: "GitHub did not let Packrat read acme/shop within 5m0s. Run packrat setup again once you have approved it."},
		{name: "a Packrat with no GitHub App stops at GitHub",
			states: []string{setupNoApp}, files: rails, plugins: pluginListed, code: 1,
			stdout: []string{"✗ GitHub       This Packrat has no GitHub App"}, absent: []string{"Plugin"}},
		{name: "signed out and not at a terminal signs in first",
			signedOut: true, states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"◷ Signed in    Not yet. Signing you in.", "✓ Signed in    dana@example.com"}, absent: []string{"Press Enter"},
			browsed: "SITE/cli/activate?code=BCDF-GHJK"},
		{name: "signed out at a terminal waits for Enter before the browser, then for Enter before GitHub",
			signedOut: true, states: []string{setupUnread, setupReadable}, files: rails, plugins: pluginListed, terminal: true, stdin: "\n\nn\n", code: 0,
			stdout: []string{"Your sign-in code is BCDF-GHJK.\nPress Enter to open Packrat in your browser and sign in...", "✓ Signed in    dana@example.com",
				"Press Enter to open GitHub and approve...", "[Y/n]", "  claude 'Set up Packrat"}, browsed: "SITE/github/setup?state=s1"},
		{name: "no origin stops before anything is asked",
			origin: "none", states: []string{setupReadable}, files: rails, code: 1,
			stdout: []string{"✗ Repository   This directory has no GitHub origin."}, absent: []string{"Platform"}, asked: []string{"GET /cli/whoami"}},
		{name: "an origin that is not on GitHub is no origin",
			origin: "git@gitlab.com:acme/shop.git", states: []string{setupReadable}, files: rails, code: 1,
			stdout: []string{"✗ Repository   This directory has no GitHub origin."}},
		{name: "an HTTPS origin names the same repository",
			origin: "https://github.com/acme/shop.git", states: []string{setupReadable}, files: rails, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Repository   acme/shop"}},
		{name: "not a Rails app says so and still connects",
			states: []string{setupReadable}, plugins: pluginListed, code: 0,
			stdout: []string{"○ Platform     Not a Rails app.", "✓ GitHub"}},
		{name: "Claude Code missing leaves the plugin and prints the command",
			states: []string{setupReadable}, files: rails, noClaude: true, terminal: true, code: 0,
			stdout: []string{"○ Plugin       Claude Code is not installed here"}, absent: []string{"[Y/n]"}},
		{name: "a plugin that is not installed is installed",
			states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", code: 0,
			stdout: []string{"✓ Plugin       Installed packrat@packrat in Claude Code"},
			ran:    []string{"claude plugin list", "claude plugin marketplace add packratlabs/claude-plugin", "claude plugin install packrat@packrat"}},
		{name: "a disabled plugin is not counted as installed",
			states: []string{setupReadable}, files: rails, plugins: pluginOff, code: 0,
			stdout: []string{"✓ Plugin       Installed packrat@packrat"}},
		{name: "a plugin install that fails says what claude said, and Claude Code is not offered",
			states: []string{setupReadable}, files: rails, plugins: "No plugins installed.\n", installFails: true, terminal: true, code: 0,
			stdout: []string{"✗ Plugin       claude plugin install packrat@packrat failed:\n               network unreachable"}, absent: []string{"[Y/n]"}},
		{name: "the gem without its initializer is not set up yet",
			states: []string{setupReadable}, files: gemOnly, plugins: pluginListed, code: 0,
			stdout: []string{"Next: have Claude Code add Packrat to this app and open the pull request."}},
		{name: "the gem and its initializer with no pull request yet",
			states: []string{setupReadable}, files: withGem, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"Next: have Claude Code open a pull request with the change that adds packrat-rails."}, absent: []string{"[Y/n]"}},
		{name: "a pull request that declared checks waits for its deploy",
			states: []string{setupDeclared}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ App          shop/development in Acme", "✓ Pull request #1, 2 checks declared", "◷ Deploy       Waiting for a production deploy that ships #1.",
				"Next: merge #1 and deploy to production."}, absent: []string{"Checks"}},
		{name: "a pull request that declared none says why",
			states: []string{setupWaived}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request #1 declared no checks: It only adds the gem."}},
		{name: "production on another version waits for the one that ships the pull request",
			states: []string{setupOtherShip}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request #1, 1 check declared", "◷ Deploy       shop/production runs 0000000. Waiting for a deploy that ships #1."}},
		{name: "production with no service.version is wrong, and says what to set",
			states: []string{setupNoVersion}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✗ Deploy       shop/production is reporting, but its traces carry no service.version.", "BUILD_COMMIT=$(git rev-parse HEAD)"}},
		{name: "checks running after the deploy",
			states: []string{setupRunning}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Pull request #1 Set up Packrat, 0 checks declared", "✓ Deploy       15655c0 went live at ", " and shipped #1", "◷ Checks       1 of 2 passed",
				"✓ Traces", "◷ Logs (until ", "Next: Packrat is running the checks of #1."}, absent: []string{"Comment"}},
		{name: "a failed check is wrong and setup is not complete",
			states: []string{setupFailed}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✗ Checks       1 of 2 passed", "✗ Logs"}, absent: []string{"Setup is complete"}},
		{name: "every check passed and Packrat commented: setup is complete",
			states: []string{setupComplete}, files: withGem, plugins: pluginListed, terminal: true, code: 0,
			stdout: []string{"✓ Checks       2 of 2 passed", "✓ Comment      https://github.com/acme/shop/pull/1#issuecomment-1",
				"Setup is complete. Packrat verifies every pull request Claude Code opens in acme/shop."}, absent: []string{"[Y/n]"}},
		{name: "checks passed but GitHub refused the comment: not complete, and it says who fixes it where",
			states: []string{setupNoComment}, files: withGem, plugins: pluginListed, code: 0,
			stdout: []string{"✓ Checks       1 of 1 passed", "✗ Comment      Packrat could not comment on #1. GitHub answered 403: Resource not accessible by integration.",
				"An owner of acme lets Packrat write pull request comments:\n               https://github.com/apps/packrat-sh/installations/7",
				"Next: let Packrat comment on pull requests."}, absent: []string{"Setup is complete"}},
		{name: "the API refusing the state is said and fails",
			states:    []string{`{"type":"error","message":"you belong to more than one org, and none of them reads acme/shop yet. Name the one to set up with org= (in the CLI, --org): org_a (Acme) and org_b (Bolt)."}`},
			stateCode: 400, files: rails, code: 1, stderr: "Name the one to set up with org= (in the CLI, --org)"},
		{name: "connecting refused is said and fails",
			states: []string{setupReadable}, connect: `{"type":"error","message":"GitHub does not let Packrat read acme/shop yet."}`, connectCode: 409, files: rails, code: 1,
			stderr: "connecting acme/shop"},
		{name: "an extra word is a usage error", args: []string{"now"}, states: []string{setupReadable}, code: 2, stderr: "the options are --no-browser and --no-claude"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { runSetupCase(t, tc) })
	}
}

func runSetupCase(t *testing.T, tc setupCase) {
	t.Helper()
	var asked []string
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.RequestURI())
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

	env := map[string]string{"PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
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
	var ran []string
	w := world{getenv: func(name string) string { return env[name] }, sleep: func(time.Duration) {}, store: store,
		stdin: strings.NewReader(tc.stdin), http: &http.Client{}, hostname: func() (string, error) { return "laptop", nil },
		origin: func() string { return origin },
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
			case tc.noClaude:
				return "", errors.New("not found")
			case command == "claude plugin list":
				return tc.plugins, nil
			case tc.installFails && strings.Contains(command, "plugin install"):
				return "Installing…\nnetwork unreachable\n", errors.New("exit 1")
			}
			return "done", nil
		},
		replace: func(name string, args ...string) error { replaced = name + " " + strings.Join(args, " "); return nil },
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
	for _, want := range tc.asked {
		if !strings.Contains(strings.Join(asked, "\n"), want) {
			t.Errorf("did not ask %q:\n%s", want, strings.Join(asked, "\n"))
		}
	}
}

func TestSetupColoursOnlyWhenAsked(t *testing.T) {
	var plain, coloured bytes.Buffer
	setupOut{w: &plain}.row(markDone, "GitHub", "ok")
	setupOut{w: &coloured, colour: true}.row(markWrong, "GitHub", "no")
	if plain.String() != "✓ GitHub       ok\n" || !strings.Contains(coloured.String(), "\x1b[31m✗\x1b[0m") {
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
	if err := json.Unmarshal(draft, &checks); err != nil || len(checks) != 2 {
		t.Fatalf("the draft is not a list of two checks: %v\n%s", err, draft)
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
