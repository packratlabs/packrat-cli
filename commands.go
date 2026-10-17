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
		return say(stderr, "packrat login: takes only --no-browser\n", exitUsage)
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
	msg := fmt.Sprintf("Open %s and type the code %s\n", started.VerificationURI, started.UserCode)
	if !*noBrowser && strings.HasPrefix(started.Complete, strings.TrimRight(siteURL(w), "/")+"/") && w.browse(started.Complete) == nil {
		msg = fmt.Sprintf("Your browser opened %s.\nCheck that it shows the code %s and sign in.\n", started.VerificationURI, started.UserCode)
	}
	if code := say(stdout, msg+"Waiting...\n", exitOK); code != exitOK {
		return code
	}
	token, err := awaitToken(w, site, started)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat login: %v\n", err), exitFailed)
	}
	if err := w.store.Save(auth.SignIn{Org: token.Org, OrgName: token.OrgName, Email: token.Email, Key: token.Key, Token: token.AccessToken}); err != nil {
		return say(stderr, fmt.Sprintf("packrat login: %v\n", err), exitFailed)
	}
	return say(stdout, fmt.Sprintf("Signed in to %s as %s.\n", token.OrgName, token.Email), exitOK)
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
		return say(stderr, "packrat logout: takes no argument\n", exitUsage)
	}
	signIn, err := w.store.Current()
	if errors.Is(err, auth.ErrNotSignedIn) {
		return say(stdout, "Not signed in.\n", exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat logout: %v\n", err), exitFailed)
	}
	if err := siteClient(w, signIn.Token).Revoke(context.Background()); err != nil && !errors.Is(err, query.ErrRefused) {
		return say(stderr, fmt.Sprintf("packrat logout: revoking the key: %v\n", err), exitFailed)
	}
	if err := w.store.Remove(signIn.Org); err != nil {
		return say(stderr, fmt.Sprintf("packrat logout: %v\n", err), exitFailed)
	}
	msg := fmt.Sprintf("Signed out of %s. Its key is revoked.\n", signIn.OrgName)
	if w.getenv(query.KeyName) != "" {
		msg += query.KeyName + " is still set, and commands still use it.\n"
	}
	return say(stdout, msg, exitOK)
}

func runAuth(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "status" {
		return say(stderr, "packrat auth: the command is packrat auth status\n", exitUsage)
	}
	key, err := query.Key(w.getenv, w.store)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	who, err := siteClient(w, key).WhoAmI(context.Background())
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	current, all, err := w.store.All()
	if err != nil && !errors.Is(err, auth.ErrNotSignedIn) {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	if err := report.SignedIn(stdout, who, w.getenv(query.KeyName) != "", current, all); err != nil {
		return say(stderr, fmt.Sprintf("packrat auth status: %v\n", err), exitFailed)
	}
	return exitOK
}

func runSQL(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	file := fs.String("f", "", "")
	quiet := fs.Bool("quiet", false, "")
	scripts, err := parseInterleaved(fs, args)
	if err != nil || len(scripts) > 1 || (len(scripts) == 1 && *file != "") {
		return say(stderr, "packrat sql: give one script, as an argument, with -f file, or on stdin\n", exitUsage)
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
		return say(stderr, fmt.Sprintf("packrat sql: reading the script: %v\n", err), exitUsage)
	}
	if len(bytes.TrimSpace(script)) == 0 {
		return say(stderr, "packrat sql: the script is empty\n", exitUsage)
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
	due := fs.String("due", "", "")
	asJSON := fs.Bool("json", false, "")
	words, err := parseInterleaved(fs, args)
	if err != nil || len(words) == 0 || len(words) > 2 || (len(words) == 2 && words[1] != "check" && words[1] != "watch") {
		return say(stderr, "packrat pr: give a pull request's number or URL, and optionally check --due <time>\n", exitUsage)
	}
	number, where, ok := pullRef(w, words[0], *app)
	if !ok {
		return say(stderr, fmt.Sprintf("packrat pr: %q is not a pull request's number or URL\n", words[0]), exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat pr: %v\n", err), exitUsage)
	}
	if len(words) == 2 {
		return askCheck(w, client, where, query.Ask{Pull: number}, *due, "pr check", "pr "+words[0]+" check --due "+*due+" --app %s", stdout, stderr)
	}
	pull, err := client.Pull(context.Background(), number, where)
	if err != nil {
		return fail(stderr, "pr", "pr "+words[0]+" --app %s", err)
	}
	if *asJSON {
		err = report.PullJSON(stdout, pull)
	} else {
		err = report.Pull(stdout, pull, client.BaseURL, width(w))
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat pr: %v\n", err), exitFailed)
	}
	return exitOK
}

func askCheck(w world, client query.Client, where query.Where, ask query.Ask, due, name, usage string, stdout, stderr io.Writer) int {
	at, err := time.Parse("2006-01-02T15:04Z07:00", due)
	if err != nil {
		at, err = time.Parse(time.RFC3339, due)
	}
	if err != nil {
		return say(stderr, "packrat "+name+": --due is the first time it can be verified, in UTC: --due 2026-10-20T21:00Z\n", exitUsage)
	}
	text, err := io.ReadAll(w.stdin)
	if err != nil || len(bytes.TrimSpace(text)) == 0 {
		return say(stderr, "packrat "+name+": write what must be true, and the SQL that shows it, on stdin\n", exitUsage)
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
		return askCheck(w, client, query.Where{App: *app}, query.Ask{Pull: *pull, Report: *reportID}, *due, "check add", "check add --due "+*due+" --app %s", stdout, stderr)
	case args[0] == "list" && len(words) == 0:
		checkes, err := client.Checkes(context.Background(), query.Where{App: *app}, *pull)
		if err == nil {
			if *asJSON {
				err = report.CheckesJSON(stdout, checkes)
			} else {
				err = report.Checkes(stdout, checkes, width(w))
			}
		}
		if err != nil {
			return fail(stderr, "check list", "check list --app %s", err)
		}
		return exitOK
	case args[0] == "show" && len(words) == 1:
		checkes, err := client.Checkes(context.Background(), query.Where{App: *app}, 0)
		if err != nil {
			return fail(stderr, "check show", "check show "+words[0]+" --app %s", err)
		}
		for _, check := range checkes.Checkes {
			if check.ID == words[0] {
				if err := report.Check(stdout, check, width(w)); err != nil {
					return say(stderr, fmt.Sprintf("packrat check show: %v\n", err), exitFailed)
				}
				return exitOK
			}
		}
		return say(stderr, fmt.Sprintf("packrat check show: %s is not open, and did not close this week, on %s\n", words[0], checkes.App), exitFailed)
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

const checkUsage = "packrat check: the commands are add, list, show and close. Run packrat help check.\n"

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
		return say(stderr, "packrat open: give a Packrat URL, or the line from a Packrat mail that holds one\n", exitUsage)
	}
	if pullURL.MatchString(address) {
		return runPR(w, []string{address}, stdout, stderr)
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
		digits = found[1]
		if found := originRe.FindStringSubmatch(strings.TrimSpace(w.origin())); found != nil {
			repo = found[1]
		}
	} else {
		return 0, query.Where{}, false
	}
	number, err := strconv.Atoi(digits)
	if err != nil {
		return 0, query.Where{}, false
	}
	return number, query.Where{App: app, Repo: repo}, true
}

func siteURL(w world) string {
	if base := w.getenv("PACKRAT_URL"); base != "" {
		return base
	}
	return query.DefaultSiteURL
}

func siteClient(w world, key string) query.Client {
	return query.Client{BaseURL: siteURL(w), Key: key, HTTP: w.http}
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
