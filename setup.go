package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/rails"
)

const (
	pluginName        = "packrat@packrat"
	pluginMarketplace = "packratlabs/claude-plugin"
	programLimit      = 2 * time.Minute
	rowName           = 13
	titleLimit        = 48
)

// exitWaiting is setupGitHub's answer when setup can go no further until a person approves on GitHub. It is not
// an exit code of the command, which ends well: it said what to do.
const exitWaiting = -1

// The marks of a setup row: done, waiting on something outside, not reached yet, and wrong.
const (
	markDone  = "✓"
	markWait  = "◷"
	markTodo  = "○"
	markWrong = "✗"
)

// setupOut prints the rows of packrat setup, one as each is known, and reads what the person answers.
type setupOut struct {
	w      io.Writer
	colour bool
	in     *bufio.Reader
}

// answer reads one line. It is false at the end of input, which is no answer rather than a yes.
func (o setupOut) answer() (string, bool) {
	line, err := o.in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)), err == nil
}

// printf writes to the terminal. A write that fails there has nowhere better to be reported.
func (o setupOut) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(o.w, format, args...)
}

func (o setupOut) row(mark, name, text string, more ...string) {
	o.printf("%s %-*s%s\n", o.paint(mark), rowName, name, text)
	for _, line := range more {
		o.printf("  %-*s%s\n", rowName, "", line)
	}
}

func (o setupOut) paint(mark string) string {
	if !o.colour {
		return mark
	}
	code := map[string]string{markDone: "32", markWait: "34", markTodo: "2", markWrong: "31"}[mark]
	return "\x1b[" + code + "m" + mark + "\x1b[0m"
}

func (o setupOut) bold(text string) string {
	if !o.colour {
		return text
	}
	return "\x1b[1m" + text + "\x1b[0m"
}

func (o setupOut) dim(text string) string {
	if !o.colour {
		return text
	}
	return "\x1b[2m" + text + "\x1b[0m"
}

// runSetupSteps does each step of setup that it can for the repository it runs in, says how far the rest have
// come, and names the next one. It is safe to run again at any time.
func runSetupSteps(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noBrowser := fs.Bool("no-browser", false, "")
	noClaude := fs.Bool("no-claude", false, "")
	if extra, err := parseInterleaved(fs, args); err != nil || len(extra) > 0 {
		return say(stderr, "packrat setup: the options are --no-browser and --no-claude. Run packrat help setup for details.\n", exitUsage)
	}
	// One reader for every question, sign-in's included: bufio.NewReader hands back a reader it is given.
	in := bufio.NewReader(w.stdin)
	w.stdin = in
	out := setupOut{w: stdout, colour: w.colour, in: in}
	out.printf("\n")

	client, email, code := setupSignIn(w, out, stdout, stderr)
	if code != exitOK {
		return code
	}
	if client.Repo == "" {
		out.row(markWrong, "Repository", "This directory has no GitHub origin.",
			"Run packrat setup in a checkout whose origin remote is on GitHub.")
		return exitFailed
	}
	out.row(markDone, "Signed in", email)
	out.row(markDone, "Repository", client.Repo+" "+out.dim("(from origin)"))
	lock := setupPlatform(w, out)

	ctx := context.Background()
	state, err := client.SetupState(ctx)
	if err != nil {
		return say(stderr, fmt.Sprintf("\npackrat setup: %v\n", err), exitFailed)
	}
	if _, code = setupGitHub(w, out, client, state, *noBrowser, stderr); code == exitWaiting {
		return exitOK
	} else if code != exitOK {
		return code
	}
	// Asked on every run: it is also what gives the org the ingest key the generator needs.
	if state, err = client.ConnectRepository(ctx); err != nil {
		return say(stderr, fmt.Sprintf("\npackrat setup: connecting %s: %v\n", client.Repo, err), exitFailed)
	}
	setupApp(out, state)
	plugin := setupPlugin(w, out)
	setupPull(out, state)
	setupDeploy(out, state)
	setupChecks(out, state)

	out.printf("\n")
	return setupNext(w, out, state, lock, plugin && !*noClaude, stderr)
}

// setupSignIn finds the sign-in, and signs in first when there is none.
func setupSignIn(w world, out setupOut, stdout, stderr io.Writer) (query.Client, string, int) {
	client, err := newClient(w)
	if errors.Is(err, query.ErrNoKey) {
		out.row(markWait, "Signed in", "Not yet. Signing you in.")
		if code := runLogin(w, nil, stdout, stderr); code != exitOK {
			return client, "", code
		}
		client, err = newClient(w)
	}
	if err != nil {
		return client, "", say(stderr, fmt.Sprintf("packrat setup: %v\n", err), exitFailed)
	}
	who, err := siteClient(w, client.Key).WhoAmI(context.Background())
	if err != nil {
		return client, "", say(stderr, fmt.Sprintf("packrat setup: %v\n", err), exitFailed)
	}
	return client, who.Email, exitOK
}

func setupPlatform(w world, out setupOut) rails.Lockfile {
	data, err := w.readFile("Gemfile.lock")
	lock := rails.ParseLockfile(data)
	gem, found := lock.Gems["rails"]
	if err != nil || !found {
		out.row(markTodo, "Platform", "Not a Rails app. See https://packrat.sh/docs/quickstart for other stacks.")
		return lock
	}
	out.row(markDone, "Platform", "Ruby on Rails "+gem.Version+" "+out.dim("(from Gemfile.lock)"))
	return lock
}

// setupGitHub opens GitHub for the person to let Packrat read the repository, and waits until it can.
func setupGitHub(w world, out setupOut, client query.Client, state query.Setup, noBrowser bool, stderr io.Writer) (query.Setup, int) {
	if state.GitHub.Readable {
		out.row(markDone, "GitHub", "Packrat can read "+client.Repo)
		return state, exitOK
	}
	// The API names a page of the site, which is another host than the API's.
	if strings.HasPrefix(state.GitHub.InstallURL, "/") {
		state.GitHub.InstallURL = strings.TrimRight(siteURL(w), "/") + state.GitHub.InstallURL
	}
	if !state.GitHub.Configured || state.GitHub.InstallURL == "" {
		out.row(markWrong, "GitHub", "This Packrat has no GitHub App, so it cannot read "+client.Repo+".")
		return state, exitFailed
	}
	out.row(markWait, "GitHub", "Packrat requires read-only code access and the ability to comment on pull requests.")
	// Nobody is at the keyboard to approve, so waiting would only hang whatever ran this.
	if !w.terminal {
		out.printf("\n  %-*sOpen this page and approve Packrat for %s:\n  %-*s%s\n\n%s approve Packrat on GitHub, then run packrat setup again.\n",
			rowName, "", client.Repo, rowName, "", state.GitHub.InstallURL, out.bold("Next:"))
		return state, exitWaiting
	}
	// The address carries who started the install, so a person who opens it by hand needs all of it.
	opened := false
	if !noBrowser {
		out.printf("\n  %-*s%s ", rowName, "", out.bold("Press Enter to open GitHub and approve..."))
		if _, entered := out.answer(); entered {
			opened = w.browse(state.GitHub.InstallURL) == nil
		}
	}
	if !opened {
		out.printf("\n  %-*sOpen this page and approve Packrat for %s:\n  %-*s%s\n", rowName, "", client.Repo, rowName, "", state.GitHub.InstallURL)
	}
	out.printf("  %-*sWaiting for you to approve Packrat for %s on GitHub...\n\n", rowName, "", client.Repo)
	for waited := time.Duration(0); waited < waitLimit; waited += pollEvery {
		w.sleep(pollEvery)
		next, err := client.SetupState(context.Background())
		if err != nil {
			return state, say(stderr, fmt.Sprintf("\npackrat setup: %v\n", err), exitFailed)
		}
		if next.GitHub.Readable {
			out.row(markDone, "GitHub", "Packrat can read "+client.Repo)
			return next, exitOK
		}
		// Approved, but for an account that does not hold the repository, or without this repository selected.
		if len(next.GitHub.Accounts) > len(state.GitHub.Accounts) {
			owner := strings.SplitN(client.Repo, "/", 2)[0]
			out.printf("  %-*sYou approved Packrat for %s, but it still cannot read %s.\n  %-*sOn GitHub, choose %s and include this repository.\n\n",
				rowName, "", strings.Join(next.GitHub.Accounts, " and "), client.Repo, rowName, "", owner)
			state = next
		}
	}
	return state, say(stderr, fmt.Sprintf("\npackrat setup: GitHub did not let Packrat read %s within %s. Run packrat setup again once you have approved it.\n",
		client.Repo, waitLimit), exitFailed)
}

func setupApp(out setupOut, state query.Setup) {
	if len(state.Apps) == 0 {
		out.row(markTodo, "App", "None yet in "+state.OrgOf.Name+". It appears when this app sends its first data.")
		return
	}
	names := make([]string, len(state.Apps))
	for i, app := range state.Apps {
		names[i] = app.App
	}
	out.row(markDone, "App", strings.Join(names, ", ")+" in "+state.OrgOf.Name)
}

// setupPlugin installs the Claude Code plugin when Claude Code is here and does not have it.
func setupPlugin(w world, out setupOut) bool {
	listed, err := w.program("claude", "plugin", "list")
	if err != nil {
		out.row(markTodo, "Plugin", "Claude Code is not installed here, so there is no plugin to install.",
			"Packrat's checks are declared by Claude Code. See https://claude.com/claude-code")
		return false
	}
	if pluginEnabled(listed) {
		out.row(markDone, "Plugin", pluginName+" is installed in Claude Code")
		return true
	}
	if said, err := w.program("claude", "plugin", "marketplace", "add", pluginMarketplace); err != nil && !strings.Contains(said, "already") {
		out.row(markWrong, "Plugin", "claude plugin marketplace add "+pluginMarketplace+" failed:", lastLine(said))
		return false
	}
	if said, err := w.program("claude", "plugin", "install", pluginName); err != nil {
		out.row(markWrong, "Plugin", "claude plugin install "+pluginName+" failed:", lastLine(said))
		return false
	}
	out.row(markDone, "Plugin", "Installed "+pluginName+" in Claude Code")
	return true
}

// pluginEnabled reads claude plugin list, which prints each plugin's name and then its status.
func pluginEnabled(listed string) bool {
	_, after, found := strings.Cut(listed, pluginName)
	for found {
		block, _, _ := strings.Cut(after, "❯")
		if strings.Contains(block, "enabled") && !strings.Contains(block, "disabled") {
			return true
		}
		_, after, found = strings.Cut(after, pluginName)
	}
	return false
}

func setupPull(out setupOut, state query.Setup) {
	pull := state.Pull
	switch {
	case pull == nil:
		out.row(markTodo, "Pull request", "None has declared checks yet.")
	case pull.None != "":
		out.row(markDone, "Pull request", fmt.Sprintf("%s declared no checks: %s", pullName(pull), pull.None))
	default:
		out.row(markDone, "Pull request", fmt.Sprintf("%s, %s declared", pullName(pull), count(len(pull.Declared), "check")))
	}
}

func pullName(pull *query.SetupPull) string {
	if pull.Title == "" {
		return fmt.Sprintf("#%d", pull.Number)
	}
	title := pull.Title
	if len(title) > titleLimit {
		title = strings.TrimSpace(title[:titleLimit]) + "…"
	}
	return fmt.Sprintf("#%d %s", pull.Number, title)
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func setupDeploy(out setupOut, state query.Setup) {
	pull, production := state.Pull, state.Production
	switch {
	case pull != nil && pull.Version != "" && pull.LiveAt != nil:
		out.row(markDone, "Deploy", fmt.Sprintf("%s went live at %s and shipped #%d", short(pull.Version), pull.LiveAt.Local().Format("15:04"), pull.Number))
	case production != nil && production.LastDataAt != nil && production.Version == "":
		out.row(markWrong, "Deploy", production.App+" is reporting, but its traces carry no service.version.",
			"Packrat cannot tell one deploy from the next, so no check can start.",
			"The gem reads BUILD_COMMIT, HEROKU_SLUG_COMMIT or SOURCE_VERSION. Set one to",
			"the commit in your deploy, such as BUILD_COMMIT=$(git rev-parse HEAD).")
	case pull != nil && production != nil:
		out.row(markWait, "Deploy", fmt.Sprintf("%s runs %s. Waiting for a deploy that ships #%d.", production.App, short(production.Version), pull.Number))
	case pull != nil:
		out.row(markWait, "Deploy", fmt.Sprintf("Waiting for a production deploy that ships #%d.", pull.Number))
	case production != nil:
		out.row(markDone, "Deploy", production.App+" runs "+short(production.Version))
	default:
		out.row(markTodo, "Deploy", "No production deploy yet.")
	}
}

func setupChecks(out setupOut, state query.Setup) {
	if state.Pull == nil || len(state.Pull.Checks) == 0 {
		return
	}
	passed, failed := 0, 0
	lines := make([]string, len(state.Pull.Checks))
	for i, check := range state.Pull.Checks {
		mark := markWait
		switch check.Result {
		case "passed":
			mark, passed = markDone, passed+1
		case "failed":
			mark, failed = markWrong, failed+1
		}
		lines[i] = out.paint(mark) + " " + check.Title
		if mark == markWait {
			lines[i] += out.dim(" (until " + check.DueAt.Local().Format("Jan 2 15:04") + ")")
		}
	}
	mark := markWait
	switch {
	case failed > 0:
		mark = markWrong
	case passed == len(lines):
		mark = markDone
	}
	out.row(mark, "Checks", fmt.Sprintf("%d of %d passed", passed, len(lines)), lines...)
	switch {
	case state.Pull.CommentURL != "":
		out.row(markDone, "Comment", state.Pull.CommentURL)
	case state.Pull.CommentError != "":
		out.row(markWrong, "Comment", fmt.Sprintf("Packrat could not comment on #%d. %s.", state.Pull.Number, strings.TrimRight(state.Pull.CommentError, ".")),
			"An owner of "+state.GitHub.Account+" lets Packrat write pull request comments:", state.GitHub.SettingsURL)
	}
}

// setupNext names the step that is left, and offers to start Claude Code when that step is Claude Code's.
func setupNext(w world, out setupOut, state query.Setup, lock rails.Lockfile, claude bool, stderr io.Writer) int {
	pull := state.Pull
	// The gem alone is half of it: the initializer is what the generator writes last.
	_, hasGem := lock.Gems["packrat-rails"]
	if _, err := w.readFile("config/initializers/packrat.rb"); err != nil {
		hasGem = false
	}
	switch {
	case pull != nil && allPassed(pull.Checks) && pull.CommentError != "":
		out.printf("%s let Packrat comment on pull requests. The checks of #%d passed, and Packrat could not say so on GitHub.\n", out.bold("Next:"), pull.Number)
	case pull != nil && allPassed(pull.Checks):
		out.printf("%s Packrat verifies every pull request Claude Code opens in %s.\n", out.bold("Setup is complete."), state.Repository)
	case pull != nil && len(pull.Checks) > 0:
		out.printf("%s Packrat is running the checks of #%d. Run packrat setup again, or packrat pr show %d.\n", out.bold("Next:"), pull.Number, pull.Number)
	case pull != nil:
		out.printf("%s merge #%d and deploy to production. Its checks start when the deploy goes live.\n", out.bold("Next:"), pull.Number)
	case hasGem:
		out.printf("%s have Claude Code open a pull request with the change that adds packrat-rails.\n", out.bold("Next:"))
	default:
		out.printf("%s have Claude Code add Packrat to this app and open the pull request.\n", out.bold("Next:"))
		return startClaude(w, out, state, claude, stderr)
	}
	return exitOK
}

func allPassed(checks []query.Check) bool {
	for _, check := range checks {
		if check.Result != "passed" {
			return false
		}
	}
	return len(checks) > 0
}

// startClaude becomes Claude Code with the prompt that sets the app up, when the person says yes.
func startClaude(w world, out setupOut, state query.Setup, claude bool, stderr io.Writer) int {
	page := strings.TrimRight(siteURL(w), "/") + state.SetupURL
	prompt := "Set up Packrat in this app. Run `packrat open " + page + "` and follow its steps."
	command := "claude " + quoteArg(prompt)
	if !claude || !w.terminal {
		out.printf("\n  %s\n\n%s\n", command, out.dim("Run packrat setup again at any time to see what is left."))
		return exitOK
	}
	out.printf("\nStart Claude Code to do that now? [Y/n] ")
	if answer, given := out.answer(); !given || (answer != "" && answer != "y" && answer != "yes") {
		out.printf("\n  %s\n\n%s\n", command, out.dim("Run packrat setup again at any time to see what is left."))
		return exitOK
	}
	if err := w.replace("claude", prompt); err != nil {
		return say(stderr, fmt.Sprintf("packrat setup: starting Claude Code: %v\n", err), exitFailed)
	}
	return exitOK
}

func short(version string) string {
	if len(version) > 7 {
		return version[:7]
	}
	return version
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func quoteArg(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func runProgram(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), programLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- claude, with arguments written here
	if err != nil {
		return string(out), fmt.Errorf("running %s: %w", name, err)
	}
	return string(out), nil
}

// becomeProgram replaces this process with the program, so that it owns the terminal.
func becomeProgram(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("finding %s: %w", name, err)
	}
	if err := syscall.Exec(path, append([]string{name}, args...), os.Environ()); err != nil { // #nosec G204 -- claude, as above
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
