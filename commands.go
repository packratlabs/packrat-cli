package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/report"
)

const slowDown = 5 * time.Second

var (
	pullURL  = regexp.MustCompile(`github\.com/([^/\s]+/[^/\s]+)/pull/(\d+)`)
	pullNum  = regexp.MustCompile(`^#?(\d+)$`)
	anyURL   = regexp.MustCompile(`https?://[^\s"'<>]+`)
	originRe = regexp.MustCompile(`github\.com[:/]([^/\s]+/[^/\s]+?)(\.git)?$`)
)

func runLogin(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noBrowser := fs.Bool("no-browser", false, "")
	if extra, err := parseInterleaved(fs, args); err != nil || len(extra) > 0 {
		return say(stderr, "packrat login: the only option is --no-browser\n", exitUsage)
	}
	site := siteClient(w, "")
	name, err := w.hostname()
	if err != nil || name == "" {
		name = "a machine"
	}
	ctx := context.Background()
	started, err := site.StartSignIn(ctx, name)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat login: %v\n", err), exitFailed)
	}
	msg := fmt.Sprintf("Open %s and enter the code %s\n", started.VerificationURI, started.UserCode)
	if !*noBrowser && strings.HasPrefix(started.Complete, strings.TrimRight(siteURL(w), "/")+"/") && w.browse(started.Complete) == nil {
		msg = fmt.Sprintf("Opened %s in your browser.\nConfirm that it shows the code %s, then sign in.\n", started.VerificationURI, started.UserCode)
	}
	if code := say(stdout, msg+"Waiting...\n", exitOK); code != exitOK {
		return code
	}
	token, err := awaitToken(w, site, started)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat login: %v\n", err), exitFailed)
	}
	if err := w.store.Save(auth.SignIn{Email: token.Email, Key: token.Key, Token: token.AccessToken}); err != nil {
		return say(stderr, fmt.Sprintf("packrat login: %v\n", err), exitFailed)
	}
	return say(stdout, fmt.Sprintf("Signed in as %s, in every org you belong to. Run packrat auth status to list them.\n", token.Email), exitOK)
}

func awaitToken(w world, site query.Client, started query.Started) (query.Token, error) {
	interval := time.Duration(started.Interval) * time.Second
	for waited := time.Duration(0); waited < time.Duration(started.ExpiresIn)*time.Second; waited += interval {
		w.sleep(interval)
		token, err := site.PollToken(context.Background(), started.DeviceCode)
		switch {
		case err == nil:
			return token, nil
		case errors.Is(err, query.ErrSlowDown):
			interval += slowDown
		case !errors.Is(err, query.ErrPending):
			return query.Token{}, fmt.Errorf("waiting for the browser: %w", err)
		}
	}
	return query.Token{}, query.ErrExpired
}

func runLogout(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return say(stderr, "packrat logout: this command takes no arguments\n", exitUsage)
	}
	signIn, err := w.store.Current()
	if errors.Is(err, auth.ErrNotSignedIn) {
		return say(stdout, "You're not signed in.\n", exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat logout: %v\n", err), exitFailed)
	}
	if err := siteClient(w, signIn.Token).Revoke(context.Background()); err != nil && !errors.Is(err, query.ErrRefused) {
		return say(stderr, fmt.Sprintf("packrat logout: revoking the key: %v\n", err), exitFailed)
	}
	if err := w.store.Remove(); err != nil {
		return say(stderr, fmt.Sprintf("packrat logout: %v\n", err), exitFailed)
	}
	msg := fmt.Sprintf("Signed out %s. The key for this machine is revoked.\n", signIn.Email)
	if w.getenv(query.KeyName) != "" {
		msg += query.KeyName + " is still set, and commands still use it.\n"
	}
	return say(stdout, msg, exitOK)
}

func runAuth(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "status" {
		return say(stderr, "packrat auth: specify a command: status. Run packrat help auth for details.\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	who, err := siteClient(w, client.Key).WhoAmI(context.Background())
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	here, why := orgHere(w, client, who)
	if err := report.SignedIn(stdout, who, w.getenv(query.KeyName) != "", here, why); err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	return exitOK
}

// orgHere is the org that commands read in this directory, and why, or nothing when they must name one.
func orgHere(w world, client query.Client, who query.Who) (here, why string) {
	switch {
	case w.org != "":
		return w.org, "--org"
	case w.getenv(orgName) != "":
		return client.Org, orgName
	case len(who.Orgs) == 1:
		return who.Orgs[0].Org, "the only org the key opens"
	case client.Repo == "":
		return "", ""
	}
	apps, err := client.Apps(context.Background())
	if err != nil {
		return "", ""
	}
	orgs := map[string]bool{}
	for _, app := range apps.Apps {
		if strings.EqualFold(app.Repository, client.Repo) {
			orgs[app.Org] = true
			here = app.Org
		}
	}
	if len(orgs) != 1 {
		return "", ""
	}
	return here, "an app in it reads " + client.Repo + ", this checkout's origin"
}

func runSQL(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	file := fs.String("f", "", "")
	quiet := fs.Bool("quiet", false, "")
	scripts, err := parseInterleaved(fs, args)
	if err != nil || len(scripts) > 1 || (len(scripts) == 1 && *file != "") {
		return say(stderr, "packrat sql: specify one query, as an argument, with -f FILE, or on stdin\n", exitUsage)
	}
	var script []byte
	switch {
	case len(scripts) == 1:
		script = []byte(scripts[0])
	case *file != "":
		script, err = os.ReadFile(*file)
	default:
		script, err = io.ReadAll(w.stdin)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat sql: reading the query: %v\n", err), exitUsage)
	}
	if len(bytes.TrimSpace(script)) == 0 {
		return say(stderr, "packrat sql: the query is empty\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat sql: %v\n", err), exitUsage)
	}
	last, err := client.SQL(context.Background(), *app, string(script), stdout)
	if err != nil {
		return fail(stderr, "sql", "sql --app %s \"select 1\"", err)
	}
	var end struct {
		Type    string  `json:"type"`
		Rows    int     `json:"rows"`
		Seconds float64 `json:"seconds"`
		App     string  `json:"app"`
		Message string  `json:"message"`
	}
	if json.Unmarshal([]byte(last), &end) == nil {
		switch {
		case end.Type == "error":
			return say(stderr, "packrat sql: "+end.Message+"\n", exitFailed)
		case end.Type == "end" && !*quiet:
			return say(stderr, fmt.Sprintf("packrat sql: %d rows in %gs from %s\n", end.Rows, end.Seconds, end.App), exitOK)
		}
	}
	return exitOK
}

func runPR(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	asJSON := fs.Bool("json", false, "")
	file := fs.String("file", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	reason := fs.String("reason", "", "")
	words, err := parseInterleaved(fs, args)
	if err == nil && len(words) >= 2 && words[0] == "checks" && words[1] == "set" {
		return declareChecks(w, words[2:], *app, *file, *dryRun, *asJSON, stdout, stderr)
	}
	if err == nil && len(words) == 2 && (words[0] == "archive" || words[0] == "unarchive") {
		return archivePull(w, words[1], *app, words[0] == "archive", *reason, *asJSON, stdout, stderr)
	}
	if err != nil || len(words) != 2 || words[0] != "show" {
		return say(stderr, prUsage, exitUsage)
	}
	number, where, ok := pullRef(w, words[1], *app)
	if !ok {
		return say(stderr, fmt.Sprintf("packrat pr show: %q is not a pull request number or URL\n", words[1]), exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat pr show: %v\n", err), exitUsage)
	}
	pull, err := client.Pull(context.Background(), number, where)
	if err != nil {
		return fail(stderr, "pr show", "pr show "+words[1]+" --app %s", err)
	}
	if *asJSON {
		err = report.PullJSON(stdout, pull)
	} else {
		err = report.Pull(stdout, pull, client.BaseURL, width(w))
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat pr show: %v\n", err), exitFailed)
	}
	return exitOK
}

func runIssues(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issues", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	asJSON := fs.Bool("json", false, "")
	var filter query.IssueFilter
	fs.StringVar(&filter.Status, "status", "", "")
	fs.StringVar(&filter.Kind, "kind", "", "")
	fs.StringVar(&filter.Environment, "environment", "", "")
	fs.StringVar(&filter.Sort, "sort", "priority", "")
	fs.StringVar(&filter.Period, "period", "", "")
	fs.IntVar(&filter.Limit, "limit", 0, "")
	var action query.IssueAction
	fs.StringVar(&action.Until, "until", "", "")
	fs.StringVar(&action.Reason, "reason", "", "")
	words, err := parseInterleaved(fs, args)
	if err == nil && len(words) == 2 && (words[0] == "resolve" || words[0] == "archive" || words[0] == "reopen") {
		action.Do = words[0]
		return actOnIssue(w, words[1], *app, action, *asJSON, stdout, stderr)
	}
	if err != nil || len(words) > 0 {
		return say(stderr, "packrat issues: specify only flags to list issues, or resolve, archive or reopen and an issue ID. Run packrat help issues for details.\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat issues: %v\n", err), exitUsage)
	}
	list, err := client.Issues(context.Background(), query.Where{App: *app, Repo: originRepo(w)}, filter)
	if err != nil {
		return fail(stderr, "issues", "issues --app %s", err)
	}
	if *asJSON {
		err = report.IssueListJSON(stdout, list)
	} else {
		err = report.IssueList(stdout, list, filter.Status, width(w))
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat issues: %v\n", err), exitFailed)
	}
	return exitOK
}

func runOverview(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	asJSON := fs.Bool("json", false, "")
	words, err := parseInterleaved(fs, args)
	if err != nil || len(words) > 0 {
		return say(stderr, "packrat overview: specify only flags. Run packrat help overview for details.\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat overview: %v\n", err), exitUsage)
	}
	overview, err := client.Overview(context.Background(), query.Where{App: *app, Repo: originRepo(w)})
	if err != nil {
		return fail(stderr, "overview", "overview --app %s", err)
	}
	if *asJSON {
		err = report.OverviewJSON(stdout, overview)
	} else {
		err = report.Overview(stdout, overview, width(w), client.BaseURL)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat overview: %v\n", err), exitFailed)
	}
	return exitOK
}

func actOnIssue(w world, id, app string, action query.IssueAction, asJSON bool, stdout, stderr io.Writer) int {
	name := "issues " + action.Do
	if action.Until != "" && (action.Do != "archive" || (action.Until != "escalating" && action.Until != "forever")) {
		return say(stderr, "packrat "+name+": --until is escalating or forever, and only for packrat issues archive\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	issue, err := client.ActOnIssue(context.Background(), id, query.Where{App: app, Repo: originRepo(w)}, action)
	if err != nil {
		return fail(stderr, name, name+" "+id+" --app %s", err)
	}
	if asJSON {
		err = report.IssueJSON(stdout, issue)
	} else {
		err = report.IssueActed(stdout, issue)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	return exitOK
}

func archivePull(w world, ref, app string, archived bool, reason string, asJSON bool, stdout, stderr io.Writer) int {
	name := "pr archive"
	if !archived {
		name = "pr unarchive"
	}
	number, where, ok := pullRef(w, ref, app)
	if !ok {
		return say(stderr, fmt.Sprintf("packrat %s: %q is not a pull request number or URL\n", name, ref), exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	pull, err := client.ArchivePull(context.Background(), number, where, archived, reason)
	if err != nil {
		return fail(stderr, name, name+" "+ref+" --app %s", err)
	}
	if asJSON {
		err = report.PullJSON(stdout, pull)
	} else {
		err = report.PullArchived(stdout, pull)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	return exitOK
}

// declareChecks sends the checks drafted in file for a pull request, or, with dryRun and no pull request,
// has the API validate the draft. A repository Packrat does not verify exits exitNotWatched, so a hook can
// let the pull request through.
func declareChecks(w world, words []string, app, file string, dryRun, asJSON bool, stdout, stderr io.Writer) int {
	const name = "pr checks set"
	if len(words) > 1 || file == "" || (len(words) == 0 && !dryRun) {
		return say(stderr, "packrat "+name+": specify the pull request and --file: packrat pr checks set NUMBER|URL --file checks.json (or --dry-run with no pull request)\n", exitUsage)
	}
	number, where := 0, query.Where{App: app, Repo: originRepo(w)}
	if len(words) == 1 {
		var ok bool
		if number, where, ok = pullRef(w, words[0], app); !ok {
			return say(stderr, fmt.Sprintf("packrat %s: %q is not a pull request number or URL\n", name, words[0]), exitUsage)
		}
	}
	var draft []byte
	var err error
	if file == "-" {
		draft, err = io.ReadAll(w.stdin)
	} else {
		draft, err = os.ReadFile(file) // #nosec G304 -- the file the user named
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: reading %s: %v\n", name, file, err), exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	declared, err := client.Declare(context.Background(), number, where, draft, dryRun)
	var notWatched *query.NotWatchedError
	if errors.As(err, &notWatched) {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, notWatched), exitNotWatched)
	}
	if err != nil {
		return fail(stderr, name, name+" --app %s", err)
	}
	if asJSON {
		err = report.DeclarationJSON(stdout, declared)
	} else {
		err = report.Declaration(stdout, declared, width(w))
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	return exitOK
}

const prUsage = "packrat pr: specify a command: show NUMBER|URL, archive, unarchive or checks set. Run packrat help pr for details.\n"

func askCheck(w world, client query.Client, where query.Where, ask query.Ask, due, name, usage string, stdout, stderr io.Writer) int {
	at, err := time.Parse("2006-01-02T15:04Z07:00", due)
	if err != nil {
		at, err = time.Parse(time.RFC3339, due)
	}
	if err != nil {
		return say(stderr, "packrat "+name+": specify --due, the earliest time the check can pass, in UTC. For example: --due 2026-10-20T21:00Z\n", exitUsage)
	}
	text, err := io.ReadAll(w.stdin)
	if err != nil || len(bytes.TrimSpace(text)) == 0 {
		return say(stderr, "packrat "+name+": provide the check on stdin: what must be true, and the SQL that shows it\n", exitUsage)
	}
	ask.Due, ask.Text = at, string(text)
	check, err := client.Check(context.Background(), where, ask)
	if err == nil {
		err = report.CheckJSON(stdout, check)
	}
	if err != nil {
		return fail(stderr, name, usage, err)
	}
	return exitOK
}

func runCheck(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return say(stderr, checkUsage, exitUsage)
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	pull := fs.Int("pull", 0, "")
	due := fs.String("due", "", "")
	reportID := fs.String("report", "", "")
	asJSON := fs.Bool("json", false, "")
	words, err := parseInterleaved(fs, args[1:])
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat check: %v\n", err), exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat check: %v\n", err), exitUsage)
	}
	switch {
	case args[0] == "add" && len(words) == 0:
		where := query.Where{App: *app}
		if *pull > 0 {
			_, where, _ = pullRef(w, strconv.Itoa(*pull), *app)
		}
		return askCheck(w, client, where, query.Ask{Pull: *pull, Report: *reportID}, *due, "check add", "check add --due "+*due+" --app %s", stdout, stderr)
	case args[0] == "list" && len(words) == 0:
		checks, err := client.Checks(context.Background(), query.Where{App: *app}, *pull)
		if err == nil {
			if *asJSON {
				err = report.ChecksJSON(stdout, checks)
			} else {
				err = report.Checks(stdout, checks, width(w))
			}
		}
		if err != nil {
			return fail(stderr, "check list", "check list --app %s", err)
		}
		return exitOK
	case args[0] == "show" && len(words) == 1:
		check, err := client.ReadCheck(context.Background(), words[0])
		if err == nil {
			if *asJSON {
				err = report.CheckJSON(stdout, check)
			} else {
				err = report.Check(stdout, check, width(w))
			}
		}
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat check show: %v\n", err), exitFailed)
		}
		return exitOK
	case args[0] == "close" && len(words) == 1:
		check, err := client.CloseCheck(context.Background(), words[0])
		if err == nil {
			err = report.CheckJSON(stdout, check)
		}
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat check close: %v\n", err), exitFailed)
		}
		return exitOK
	default:
		return say(stderr, checkUsage, exitUsage)
	}
}

const checkUsage = "packrat check: specify a command: add, list, show or close. Run packrat help check for details.\n"

// width is the terminal's, from COLUMNS, which shells set; 100 where nothing says.
func width(w world) int {
	if columns, err := strconv.Atoi(w.getenv("COLUMNS")); err == nil && columns >= 40 {
		return columns
	}
	return 100
}

func runOpen(w world, args []string, stdout, stderr io.Writer) int {
	address := anyURL.FindString(strings.Join(args, " "))
	if address == "" {
		return say(stderr, "packrat open: specify a Packrat URL, or a pasted line that contains one\n", exitUsage)
	}
	if pullURL.MatchString(address) {
		return runPR(w, []string{"show", address}, stdout, stderr)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat open: %v\n", err), exitUsage)
	}
	body, _, err := client.Page(context.Background(), address, client.BaseURL, siteURL(w))
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat open: %v\n", err), exitFailed)
	}
	if _, err := stdout.Write(body); err != nil {
		return say(stderr, fmt.Sprintf("packrat open: %v\n", err), exitFailed)
	}
	return exitOK
}

// fail says why a command that asked Packrat failed. A request that needed an app is said the way
// heroku says it: what is missing and which apps there are, then the command again with --app.
func fail(stderr io.Writer, name, usage string, err error) int {
	var noApp *query.AppRequiredError
	if errors.As(err, &noApp) {
		return say(stderr, fmt.Sprintf("Error: %v\nUSAGE: packrat "+usage+"\n", noApp, "<app-name>"), exitUsage)
	}
	return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
}

// pullRef reads a pull request's URL or number. The app is --app, else the repository of the URL,
// else the repository of this checkout's origin remote.
func pullRef(w world, ref, app string) (int, query.Where, bool) {
	var repo, digits string
	if found := pullURL.FindStringSubmatch(ref); found != nil {
		repo, digits = found[1], found[2]
	} else if found := pullNum.FindStringSubmatch(ref); found != nil {
		digits, repo = found[1], originRepo(w)
	} else {
		return 0, query.Where{}, false
	}
	number, err := strconv.Atoi(digits)
	if err != nil {
		return 0, query.Where{}, false
	}
	return number, query.Where{App: app, Repo: repo}, true
}

// originRepo is the GitHub owner/name of this checkout's origin remote, or empty.
func originRepo(w world) string {
	if w.origin == nil {
		return ""
	}
	if found := originRe.FindStringSubmatch(strings.TrimSpace(w.origin())); found != nil {
		return found[1]
	}
	return ""
}

func siteURL(w world) string {
	if base := w.getenv("PACKRAT_URL"); base != "" {
		return base
	}
	return query.DefaultSiteURL
}

func siteClient(w world, key string) query.Client {
	return query.Client{BaseURL: siteURL(w), Key: key, HTTP: w.http, Version: version, Agent: query.Agent(w.getenv), Heard: w.heard}
}

func gitOrigin() string {
	ctx, cancel := context.WithTimeout(context.Background(), httpLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// browse opens the sign-in page in the person's browser.
func browse(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpLimit)
	defer cancel()
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", address) // #nosec G204 -- the sign-in page the API named
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", address) // #nosec G204 -- as above
	default:
		command = exec.CommandContext(ctx, "xdg-open", address) // #nosec G204 -- as above
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("opening a browser: %w", err)
	}
	return nil
}
