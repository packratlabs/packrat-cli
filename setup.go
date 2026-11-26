package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/rails"
	"github.com/packratlabs/packrat-cli/internal/report"
)

const (
	pluginName        = "packrat@packrat"
	pluginMarketplace = "packratlabs/claude-plugin"
	programLimit      = 2 * time.Minute
	reportLimit       = 3 * time.Second
	rowName           = 16
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

var markWords = map[string]string{markDone: "done", markWait: "wait", markTodo: "todo", markWrong: "wrong"}

// setupOut prints the rows of packrat setup, one as each is known, and reads what the person answers.
type setupOut struct {
	w      io.Writer
	colour bool
	in     *bufio.Reader
	// yes answers every question, and terminal says whether anyone is there to ask.
	yes, terminal bool
	screen        *screen
	log           *setupLog
}

// setupLog is what one run of packrat setup printed, which the site keeps: no other request shows where a run on
// someone's machine stopped. It is sent before the run waits on the person or hands off to Claude Code, and as it
// ends, each time whole, so the newest report of a run is all of it.
type setupLog struct {
	client *query.Client
	steps  []query.SetupStep
	sent   int
}

// add keeps a row. The signed-in row's text is left out, because it is the person's email.
func (l *setupLog) add(mark, name, text string, more []string) {
	if l == nil {
		return
	}
	step := query.SetupStep{Step: name, Mark: markWords[mark]}
	if mark != markDone || name != "Signed in" {
		step.Text = colourCode.ReplaceAllString(strings.TrimSpace(text+" "+strings.Join(more, " ")), "")
	}
	l.steps = append(l.steps, step)
}

// waiting sends the rows when there are new ones since the last report.
func (l *setupLog) waiting() {
	if l != nil && l.sent < len(l.steps) {
		l.send(query.SetupReport{Phase: "waiting"})
	}
}

func (l *setupLog) ended(phase string, code int, failure string) {
	if l != nil {
		l.send(query.SetupReport{Phase: phase, Exit: &code, Error: failure})
	}
}

// send gives the site a few seconds, and says nothing when it does not answer: the report is never what setup is for.
func (l *setupLog) send(report query.SetupReport) {
	if l.client == nil {
		return
	}
	l.sent = len(l.steps)
	report.Steps = l.steps
	ctx, cancel := context.WithTimeout(context.Background(), reportLimit)
	defer cancel()
	_ = l.client.ReportSetup(ctx, report)
}

// newSetupRun names a run of packrat setup, unless it runs inside one (Claude Code started by setup runs it again).
func newSetupRun(getenv func(string) string) string {
	if run := query.SetupRun(getenv); run != "" {
		return run
	}
	var random [8]byte
	_, _ = rand.Read(random[:])
	return hex.EncodeToString(random[:])
}

// screen counts the terminal lines written since a row started to wait, so the row can be drawn again in place.
// A width of 0 means the lines cannot be counted, and nothing is redrawn.
type screen struct {
	width, lines, column int
}

var colourCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// wrote counts the lines of text, each as many terminal lines as it wraps to.
func (s *screen) wrote(text string) {
	for i, part := range strings.Split(colourCode.ReplaceAllString(text, ""), "\n") {
		if i > 0 {
			s.lines += 1 + max(s.column-1, 0)/s.width
			s.column = 0
		}
		s.column += utf8.RuneCountInString(part)
	}
}

// mark starts the count at a row that will wait for the person.
func (o setupOut) mark() {
	o.screen.lines, o.screen.column = 0, 0
}

// redraws says whether a waiting row is replaced by its result, rather than followed by it.
func (o setupOut) redraws() bool {
	return o.terminal && o.screen.width > 0
}

// rewind erases everything written since mark, so the row's result takes the place of the row that waited.
func (o setupOut) rewind() {
	if o.redraws() && o.screen.lines > 0 {
		_, _ = fmt.Fprintf(o.w, "\x1b[%dA\r\x1b[J", o.screen.lines)
	}
	o.mark()
}

// confirm asks a yes-or-no question, where Enter is yes. Nothing outside the project changes unless a person says
// so: with nobody at the keyboard and no --yes, the answer is no.
func (o setupOut) confirm(question string) bool {
	if o.yes {
		return true
	}
	if !o.terminal {
		return false
	}
	o.printf("\n  %-*s%s [Y/n] ", rowName, "", o.bold(question))
	answer, given := o.answer()
	o.printf("\n")
	return given && (answer == "" || answer == "y" || answer == "yes")
}

// answer reads one line. It is false at the end of input, which is no answer rather than a yes.
func (o setupOut) answer() (string, bool) {
	o.log.waiting()
	line, err := o.in.ReadString('\n')
	if err == nil && o.redraws() {
		// The terminal echoes the Enter that ends the answer.
		o.screen.wrote("\n")
	}
	return strings.ToLower(strings.TrimSpace(line)), err == nil
}

// printf writes to the terminal. A write that fails there has nowhere better to be reported.
func (o setupOut) printf(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	_, _ = io.WriteString(o.w, text)
	if o.redraws() {
		o.screen.wrote(text)
	}
}

func (o setupOut) row(mark, name, text string, more ...string) {
	o.log.add(mark, name, text, more)
	o.draw(mark, name, text, more...)
}

func (o setupOut) draw(mark, name, text string, more ...string) {
	o.printf("%s %-*s%s\n", o.paint(mark), rowName, name, text)
	for _, line := range more {
		if line == "" {
			o.printf("\n")
			continue
		}
		o.printf("  %-*s%s\n", rowName, "", line)
	}
}

// setupRows is every row of setup in order, with what each will show, for the rows a run has not reached.
var setupRows = [][2]string{
	{"Signed in", "Your Packrat account"},
	{"Repository", "The GitHub repository of this checkout"},
	{"Platform", "The app's framework"},
	{"GitHub", "You approve Packrat for the repository"},
	{"Plugin", "Claude Code Packrat Plugin"},
	{"Data", "Receiving telemetry data from the app"},
	{"Pull request", "Opened by Claude Code"},
	{verification, "Packrat runs its first PR verification"},
}

// verification stands for the deploy, the checks and the comment until there is a pull request to verify.
const verification = "PR Verification"

// rest prints the rows after the named one as not reached, so a run that stops to wait shows what is left.
func (o setupOut) rest(after string) {
	reached := false
	for _, row := range setupRows {
		if reached {
			o.draw(markTodo, row[0], o.dim(row[1]))
		}
		reached = reached || row[0] == after
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
	run := newSetupRun(w.getenv)
	getenv := w.getenv
	w.getenv = func(name string) string {
		if name == query.SetupRunEnv {
			return run
		}
		return getenv(name)
	}
	log := &setupLog{}
	var said bytes.Buffer
	code := setupSteps(w, log, args, stdout, io.MultiWriter(stderr, &said))
	log.ended("exit", code, lastLine(said.String()))
	return code
}

func setupSteps(w world, log *setupLog, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noBrowser := fs.Bool("no-browser", false, "")
	noClaude := fs.Bool("no-claude", false, "")
	fs.BoolVar(&w.yes, "yes", false, "")
	fs.BoolVar(&w.yes, "y", false, "")
	app := fs.String("app", "", "")
	isNew := fs.Bool("new", false, "")
	if extra, err := parseInterleaved(fs, args); err != nil || len(extra) > 0 || *app != "" && *isNew {
		return say(stderr, "packrat setup: the options are --app or --new, --yes, --no-browser and --no-claude. Run packrat help setup for details.\n", exitUsage)
	}
	// One reader for every question, sign-in's included: bufio.NewReader hands back a reader it is given.
	in := bufio.NewReader(w.stdin)
	w.stdin = in
	out := setupOut{w: stdout, colour: w.colour, in: in, yes: w.yes, terminal: w.terminal, screen: &screen{}, log: log}
	if w.width != nil {
		out.screen.width = w.width()
	}
	out.printf("\n")

	client, email, code := setupSignIn(w, out, stdout, stderr)
	if code != exitOK {
		return code
	}
	site := siteClient(w, client.Key)
	log.client = &site
	out.row(markDone, "Signed in", email)
	if client.Repo == "" {
		setupNoRepository(w, out)
		setupPlatform(w, out)
		out.rest("Platform")
		return exitFailed
	}
	out.row(markDone, "Repository", client.Repo+" "+out.dim("(from origin)"))
	lock, next := setupPlatform(w, out)
	// Stopping here leaves the repository unconnected, so its pull requests are not held for data it cannot send.
	if next != "" {
		out.rest("Platform")
		out.printf("\n%s %s\n", out.bold("Next:"), next)
		return exitFailed
	}

	// Setup is the site's: it holds the GitHub App, and the pages this names are its own.
	client = siteClient(w, client.Key).In(client.Org, client.Repo)
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
	service, how, next := setupExisting(w, out, client, state, *app, *isNew)
	if next != "" {
		out.rest("GitHub")
		out.printf("\n%s %s\n", out.bold("Next:"), next)
		return exitFailed
	}
	// Asked on every run: it is also what gives the org the ingest key the generator needs.
	if state, err = client.ConnectRepository(ctx, service); err != nil {
		return say(stderr, fmt.Sprintf("\npackrat setup: connecting %s: %v\n", client.Repo, err), exitFailed)
	}
	if service != "" {
		out.row(markDone, "App", service+" is connected to "+client.Repo+how)
	}
	plugin := setupPlugin(w, out)
	setupApp(out, state)
	setupPull(out, state)
	if state.Pull == nil {
		out.rest("Pull request")
	} else {
		setupVerification(out, state)
	}

	out.printf("\n")
	return setupNext(w, out, state, lock, plugin && !*noClaude, stderr)
}

// setupExisting finds which of the org's apps with no repository this checkout is: the one --app names, the one
// config/application.rb names, or the one the person picks. An app that reported before setup ran is found by no
// repository, so without an answer setup would wait for a new app that never comes. service is "" for a new app; how
// says why the app was chosen when nobody picked it. next is set when setup cannot tell and has nobody to ask.
func setupExisting(w world, out setupOut, client query.Client, state query.Setup, app string, isNew bool) (service, how, next string) {
	apps := state.Unconnected
	names := make([]string, len(apps))
	for i, each := range apps {
		names[i] = each.Service
	}
	switch {
	case isNew:
		return "", "", ""
	case app != "" && slices.Contains(names, app):
		return app, "", ""
	case app != "" && len(names) == 0:
		out.row(markWrong, "App", "This org has no app without a repository, so there is no "+app+" to connect.")
		return "", "", "run packrat setup again without --app."
	case app != "":
		out.row(markWrong, "App", app+" is not an app of this org with no repository.", "Those are: "+strings.Join(names, ", ")+".")
		return "", "", "run packrat setup --app with one of them, or --new if this repository is a new app."
	case len(apps) == 0:
		return "", "", ""
	}
	named := ""
	if data, err := w.readFile(rails.ApplicationFile); err == nil {
		named = rails.AppName(data)
	}
	if slices.Contains(names, named) {
		return named, " " + out.dim("(named in "+rails.ApplicationFile+")"), ""
	}
	if out.yes || !out.terminal {
		detail := rails.ApplicationFile + " does not name this app, so setup cannot tell which it is."
		if named != "" {
			detail = "None of them is " + named + ", the name " + rails.ApplicationFile + " gives this app."
		}
		out.row(markTodo, "App", "Packrat has apps with no repository: "+strings.Join(names, ", ")+".", detail)
		return "", "", "ask which of them this repository is, then run packrat setup --app <app>, or packrat setup --new if it is a new app."
	}
	width := 0
	for _, app := range apps {
		width = max(width, utf8.RuneCountInString(app.Service))
	}
	out.printf("\n  %-*sPackrat already receives data from apps that have no repository:\n\n", rowName, "")
	for i, app := range apps {
		last := ""
		if app.LastDataAt != nil {
			last = " · last data " + report.Ago(state.Now.Sub(*app.LastDataAt))
		}
		out.printf("  %-*s  %d  %-*s  %s%s\n", rowName, "", i+1, width, app.Service, strings.Join(app.Envs, ", "), last)
	}
	out.printf("  %-*s  %d  Neither: this is a new app\n", rowName, "", len(apps)+1)
	for {
		out.printf("\n  %-*s%s ", rowName, "", out.bold(fmt.Sprintf("Which app is %s? [1-%d]", client.Repo, len(apps)+1)))
		answer, given := out.answer()
		if !given {
			out.printf("\n")
			return "", "", ""
		}
		if pick, err := strconv.Atoi(answer); err == nil && pick >= 1 && pick <= len(apps)+1 {
			out.printf("\n")
			if pick > len(apps) {
				return "", "", ""
			}
			return apps[pick-1].Service, "", ""
		}
	}
}

// setupSignIn finds the sign-in, and signs in first when there is none.
func setupSignIn(w world, out setupOut, stdout, stderr io.Writer) (query.Client, string, int) {
	client, err := newClient(w)
	if errors.Is(err, query.ErrNoKey) {
		out.row(markWait, "Signed in", "Not yet. Signing you in.")
		out.rest("Signed in")
		out.printf("\n")
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

// setupNoRepository says why this checkout has no GitHub repository, and gives the commands that put it on GitHub.
func setupNoRepository(w world, out setupOut) {
	origin := ""
	if w.origin != nil {
		origin = strings.TrimSpace(w.origin())
	}
	if origin != "" {
		out.row(markWrong, "Repository", "This checkout's origin is "+origin+", which is not on GitHub.",
			"Packrat verifies pull requests on GitHub only.")
		return
	}
	text := "This app is not on GitHub yet."
	more := []string{"Packrat verifies your pull requests, so it needs the app's GitHub repository."}
	var steps []string
	if _, err := w.program("git", "rev-parse", "--is-inside-work-tree"); err != nil {
		text = "This directory is not a git repository."
		more = []string{"Run packrat setup in your app's directory. If this is your app, Packrat needs it on GitHub."}
		steps = append(steps, "git init")
	}
	if _, err := w.program("git", "rev-parse", "--verify", "HEAD"); err != nil {
		steps = append(steps, `git add -A && git commit -m "First commit"`)
	}
	if _, err := w.program("gh", "--version"); err == nil {
		more = append(more, "To create a private repository and push the app to it, run:")
		steps = append(steps, "gh repo create --private --source . --push")
	} else {
		more = append(more, "Create a private repository at https://github.com/new, then run:")
		steps = append(steps, "git remote add origin <the repository's URL>", "git push -u origin HEAD")
	}
	more = append(more, "")
	for _, step := range steps {
		more = append(more, "  "+step)
	}
	more = append(more, "", "Then run packrat setup again.")
	out.row(markWrong, "Repository", text, more...)
}

// minimumRails is the oldest Rails that OpenTelemetry's Rails instrumentation, and so packrat-rails, runs on.
const minimumRails = "7.1"

const quickstartURL = "https://packrat.sh/docs/quickstart"

// setupPlatform says the next step when setup cannot go on, because the app cannot take packrat-rails.
func setupPlatform(w world, out setupOut) (rails.Lockfile, string) {
	data, err := w.readFile("Gemfile.lock")
	lock := rails.ParseLockfile(data)
	gem, found := lock.Gems["rails"]
	if err != nil || !found {
		out.row(markWrong, "Platform", "Not a Rails app.", "packrat setup sets up Rails apps. For other stacks, see "+quickstartURL)
		return lock, "send OpenTelemetry from the app to Packrat: " + quickstartURL
	}
	text := "Ruby on Rails " + gem.Version + " " + out.dim("(from Gemfile.lock)")
	if olderThan(gem.Version, minimumRails) {
		out.row(markWrong, "Platform", text, "Packrat requires Rails "+minimumRails+" or later.")
		return lock, "upgrade the app to Rails " + minimumRails + " or later, then run packrat setup again."
	}
	out.row(markDone, "Platform", text)
	return lock, ""
}

// olderThan compares the major and minor parts. A version it cannot read is not older.
func olderThan(version, minimum string) bool {
	parts := func(v string) (int, int, bool) {
		fields := strings.SplitN(v, ".", 3)
		if len(fields) < 2 {
			return 0, 0, false
		}
		major, errMajor := strconv.Atoi(fields[0])
		minor, errMinor := strconv.Atoi(fields[1])
		return major, minor, errMajor == nil && errMinor == nil
	}
	major, minor, ok := parts(version)
	wantMajor, wantMinor, _ := parts(minimum)
	return ok && (major < wantMajor || major == wantMajor && minor < wantMinor)
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
		out.rest("GitHub")
		return state, exitFailed
	}
	out.mark()
	out.row(markWait, "GitHub", "Packrat requires read-only code access and permission to comment on pull requests.")
	out.rest("GitHub")
	// Nobody is at the keyboard to approve, so waiting would only hang whatever ran this.
	if !w.terminal {
		out.printf("\n  %-*sOpen this page and approve Packrat for %s:\n  %-*s%s\n\n%s approve Packrat on GitHub, then run packrat setup again.\n",
			rowName, "", client.Repo, rowName, "", state.GitHub.InstallURL, out.bold("Next:"))
		return state, exitWaiting
	}
	// The address carries who started the install, so a person who opens it by hand needs all of it.
	opened := false
	switch {
	case noBrowser:
	case out.yes:
		opened = w.browse(state.GitHub.InstallURL) == nil
	default:
		out.printf("\n  %-*s%s ", rowName, "", out.bold("Press Enter to open GitHub and approve..."))
		if _, entered := out.answer(); entered {
			opened = w.browse(state.GitHub.InstallURL) == nil
		}
	}
	if !opened {
		out.printf("\n  %-*sOpen this page and approve Packrat for %s:\n  %-*s%s\n", rowName, "", client.Repo, rowName, "", state.GitHub.InstallURL)
	}
	out.printf("  %-*sWaiting for you to approve Packrat for %s on GitHub...\n\n", rowName, "", client.Repo)
	out.log.waiting()
	for waited := time.Duration(0); waited < waitLimit; waited += pollEvery {
		w.sleep(pollEvery)
		next, err := client.SetupState(context.Background())
		if err != nil {
			return state, say(stderr, fmt.Sprintf("\npackrat setup: %v\n", err), exitFailed)
		}
		if next.GitHub.Readable {
			out.rewind()
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
		out.row(markTodo, "Data", "None yet. Install packrat-rails")
		return
	}
	names := make([]string, len(state.Apps))
	for i, app := range state.Apps {
		names[i] = app.App
	}
	out.row(markDone, "Data", "Receiving from "+strings.Join(names, ", "))
}

// setupPlugin installs the Claude Code plugin when Claude Code is here and does not have it, once the person agrees:
// it changes Claude Code for every project on the machine, not this one.
func setupPlugin(w world, out setupOut) bool {
	listed, err := w.program("claude", "plugin", "list")
	// Claude Code is often installed in another shell than the one running setup, such as WSL beside PowerShell.
	if errors.Is(err, exec.ErrNotFound) {
		out.row(markTodo, "Plugin", "Claude Code is not on this shell's PATH, so setup cannot install the plugin.",
			"Run packrat setup in the shell where you run claude, or install Claude Code: https://claude.com/claude-code")
		return false
	}
	if err != nil {
		reason := lastLine(listed)
		if reason == "" {
			reason = err.Error()
		}
		out.row(markWrong, "Plugin", "claude plugin list failed:", reason)
		return false
	}
	if pluginEnabled(listed) {
		out.row(markDone, "Plugin", pluginName+" is installed in Claude Code")
		return true
	}
	out.mark()
	if !out.yes && out.terminal {
		out.row(markWait, "Plugin", "Connects Claude Code to Packrat.")
	}
	agreed := out.confirm("Install the Packrat plugin in Claude Code?")
	out.rewind()
	if !agreed {
		again := "Run packrat setup again to install it."
		if !out.terminal {
			again = "Run packrat setup --yes to install it."
		}
		out.row(markTodo, "Plugin", "Not installed. "+again)
		return false
	}
	if out.redraws() {
		out.row(markWait, "Plugin", "Installing "+pluginName+" in Claude Code...")
	}
	if said, err := w.program("claude", "plugin", "marketplace", "add", pluginMarketplace); err != nil && !strings.Contains(said, "already") {
		out.rewind()
		out.row(markWrong, "Plugin", "claude plugin marketplace add "+pluginMarketplace+" failed:", lastLine(said))
		return false
	}
	if said, err := w.program("claude", "plugin", "install", pluginName); err != nil {
		out.rewind()
		out.row(markWrong, "Plugin", "claude plugin install "+pluginName+" failed:", lastLine(said))
		return false
	}
	out.rewind()
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
		out.row(markTodo, "Pull request", out.dim("Opened by Claude Code"))
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

// setupVerification is the pull request's verification in one line: waiting for the deploy that ships it, then how
// many of its checks have passed. packrat pr show has each check, so setup does not repeat them.
func setupVerification(out setupOut, state query.Setup) {
	pull, production := state.Pull, state.Production
	if pull.Version == "" || pull.LiveAt == nil {
		if production != nil && production.LastDataAt != nil && production.Version == "" {
			out.row(markWrong, verification, production.App+" is reporting, but its traces carry no service.version.",
				"Packrat cannot tell one deploy from the next, so no check can start.",
				"The gem reads BUILD_COMMIT, HEROKU_SLUG_COMMIT or SOURCE_VERSION. Set one to",
				"the commit in your deploy, such as BUILD_COMMIT=$(git rev-parse HEAD).")
			return
		}
		out.row(markWait, verification, fmt.Sprintf("Waiting for a production deploy that ships #%d.", pull.Number))
		return
	}
	passed, failed := 0, 0
	for _, check := range pull.Checks {
		switch check.Result {
		case "passed":
			passed++
		case "failed":
			failed++
		}
	}
	text := fmt.Sprintf("%d of %s passed", passed, count(len(pull.Checks), "check"))
	switch {
	case failed > 0:
		out.row(markWrong, verification, fmt.Sprintf("%s. See packrat pr show %d", text, pull.Number))
	case allPassed(pull.Checks):
		out.row(markDone, verification, text)
	default:
		out.row(markWait, verification, fmt.Sprintf("%s. See packrat pr show %d", text, pull.Number))
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
	case pull != nil && allPassed(pull.Checks):
		out.printf("%s Packrat verifies every pull request Claude Code opens in %s.\nIn Claude Code, type /packrat to see how each one did in production.\n",
			out.bold("Setup is complete."), state.Repository)
	case pull != nil && len(pull.Checks) > 0:
		out.printf("%s Packrat is running the checks of #%d. Run packrat setup again, or packrat pr show %d.\n", out.bold("Next:"), pull.Number, pull.Number)
	case pull != nil:
		out.printf("%s merge #%d and deploy to production. Its checks start when the deploy goes live.\n", out.bold("Next:"), pull.Number)
	case hasGem:
		out.printf("%s have Claude Code open a pull request with the change that adds packrat-rails.\n", out.bold("Next:"))
	default:
		out.printf("%s have Claude Code create a pull request to install Packrat.\n", out.bold("Next:"))
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
	// Quoted, because the ? in the page's address is a glob to zsh.
	open := `packrat open "` + page + `"`
	prompt := "Set up Packrat in this app. Run `" + open + "` and follow its steps."
	// A coding agent is already the Claude Code this step needs, so it does the step rather than start another.
	if query.Agent(w.getenv) != "" {
		out.printf("\n  Run `%s` and follow its steps.\n", open)
		return exitOK
	}
	command := "claude " + quoteArg(prompt)
	if !claude || !w.terminal {
		out.printf("\n  %s\n\n%s\n", command, out.dim("Run packrat setup again at any time to see what is left."))
		return exitOK
	}
	if !out.yes {
		out.printf("\nStart Claude Code to do that now? [Y/n] ")
		if answer, given := out.answer(); !given || (answer != "" && answer != "y" && answer != "yes") {
			out.printf("\n  %s\n\n%s\n", command, out.dim("Run packrat setup again at any time to see what is left."))
			return exitOK
		}
	}
	out.log.ended("claude", exitOK, "")
	if err := w.replace([]string{query.SetupRunEnv + "=" + query.SetupRun(w.getenv)}, "claude", prompt); err != nil {
		return say(stderr, fmt.Sprintf("packrat setup: starting Claude Code: %v\n", err), exitFailed)
	}
	return exitOK
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

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
