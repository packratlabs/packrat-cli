// Command packrat is the CLI to interact with Packrat.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
	"github.com/packratlabs/packrat-cli/internal/detect"
	"github.com/packratlabs/packrat-cli/internal/host"
	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/rails"
	"github.com/packratlabs/packrat-cli/internal/report"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

const (
	pollEvery   = 3 * time.Second
	waitLimit   = 5 * time.Minute
	httpLimit   = 30 * time.Second
	updateLimit = 5 * time.Minute
)

// world is what a command reads from outside its arguments. Tests give their own.
type world struct {
	getenv   func(string) string
	sleep    func(time.Duration)
	store    auth.Store
	stdin    io.Reader
	http     *http.Client
	hostname func() (string, error)
	origin   func() string
	browse   func(string) error
	now      func() time.Time
	// heard collects what the API says about releases, for the notice after the command.
	heard      *query.Heard
	releases   query.Releases
	executable func() (string, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	w := world{getenv: os.Getenv, sleep: time.Sleep, stdin: os.Stdin, http: &http.Client{Timeout: httpLimit},
		hostname: os.Hostname, origin: gitOrigin, browse: browse, now: time.Now, heard: &query.Heard{},
		releases:   query.Releases{API: query.ReleasesAPI, Download: query.ReleasesDownload, HTTP: &http.Client{Timeout: updateLimit}},
		executable: os.Executable}
	dir, err := auth.Dir(os.Getenv)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat: %v\n", err), exitFailed)
	}
	w.store = auth.Store{Dir: dir}
	if runtime.GOOS == "darwin" {
		w.store.Secrets = auth.Keychain{}
	}
	return runIn(w, args, stdout, stderr)
}

func runIn(w world, args []string, stdout, stderr io.Writer) int {
	code := dispatch(w, args, stdout, stderr)
	notify(w, args, stderr)
	return code
}

func dispatch(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return say(stderr, topLevelHelp(), exitUsage)
	}
	if words, asked := helpAsked(args); asked {
		text, found := helpFor(words)
		if !found {
			return say(stderr, fmt.Sprintf("packrat: no help for %q. Run packrat help to see all commands.\n", strings.Join(words, " ")), exitUsage)
		}
		return say(stdout, text, exitOK)
	}
	switch args[0] {
	case "setup":
		return runSetup(w, args[1:], stdout, stderr)
	case "apps":
		return runApps(w, args[1:], stdout, stderr)
	case "status":
		return runStatus(w, args[1:], stdout, stderr)
	case "login":
		return runLogin(w, args[1:], stdout, stderr)
	case "logout":
		return runLogout(w, args[1:], stdout, stderr)
	case "auth":
		return runAuth(w, args[1:], stdout, stderr)
	case "sql":
		return runSQL(w, args[1:], stdout, stderr)
	case "pr":
		return runPR(w, args[1:], stdout, stderr)
	// watch is the name before 2026-10-17.
	case "check", "watch":
		return runCheck(w, args[1:], stdout, stderr)
	case "open":
		return runOpen(w, args[1:], stdout, stderr)
	case "update":
		return runUpdate(w, args[1:], stdout, stderr)
	default:
		return say(stderr, fmt.Sprintf("packrat: %q is not a packrat command. Run packrat help to see all commands.\n", args[0]), exitUsage)
	}
}

func runApps(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	extra, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return say(stdout, topLevelHelp(), exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat apps: %v\n", err), exitUsage)
	}
	if len(extra) > 0 {
		return say(stderr, "packrat apps: this command takes no arguments\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat apps: %v\n", err), exitUsage)
	}
	apps, err := client.Apps(context.Background())
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat apps: %v\n", err), exitFailed)
	}
	write := report.Apps
	if *asJSON {
		write = report.AppsJSON
	}
	if err := write(stdout, apps); err != nil {
		return say(stderr, fmt.Sprintf("packrat apps: %v\n", err), exitFailed)
	}
	return exitOK
}

func runStatus(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", "", "")
	wait := fs.Bool("wait", false, "")
	asJSON := fs.Bool("json", false, "")
	extra, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return say(stdout, topLevelHelp(), exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitUsage)
	}
	if len(extra) > 0 {
		return say(stderr, "packrat status: specify the app with --app SERVICE/ENV, not as an argument. Run packrat apps to list your apps.\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitUsage)
	}
	var status query.Status
	switch {
	case *wait && *app == "":
		if !*asJSON {
			if code := say(stdout, "Waiting for data from your app. Start your app and load a page.\n\n", exitOK); code != exitOK {
				return code
			}
		}
		var name string
		if name, err = awaitApp(w, client); err == nil {
			status, err = client.Status(context.Background(), name)
		}
	case *wait:
		if !*asJSON {
			if code := say(stdout, fmt.Sprintf("Waiting for data from %s. Start your app and load a page.\n\n", *app), exitOK); code != exitOK {
				return code
			}
		}
		status, err = awaitData(w, client, *app)
	case *app == "":
		return statusOfEach(client, *asJSON, stdout, stderr)
	default:
		status, err = client.Status(context.Background(), *app)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitFailed)
	}
	write := report.Status
	if *asJSON {
		write = report.StatusJSON
	}
	if err := write(stdout, status); err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitFailed)
	}
	if !report.Sending(status) {
		return exitFailed
	}
	return exitOK
}

// statusOfEach is packrat status with no app named: the status of every app the key opens, one
// after another. With --json, one app is its status document and several are a list of them.
func statusOfEach(client query.Client, asJSON bool, stdout, stderr io.Writer) int {
	names, err := appNames(client)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitFailed)
	}
	statuses := make([]query.Status, 0, len(names))
	for _, name := range names {
		status, err := client.Status(context.Background(), name)
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat status: asking for %s: %v\n", name, err), exitFailed)
		}
		statuses = append(statuses, status)
	}
	switch {
	case asJSON && len(statuses) == 1:
		err = report.StatusJSON(stdout, statuses[0])
	case asJSON:
		err = report.StatusesJSON(stdout, statuses)
	default:
		err = report.Statuses(stdout, statuses)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitFailed)
	}
	for _, status := range statuses {
		if !report.Sending(status) {
			return exitFailed
		}
	}
	return exitOK
}

var (
	errNoNewData = errors.New("no new data arrived within 5 minutes")
	errNoApps    = errors.New("no app has sent data yet. Run packrat status --wait, then start your app")
)

// appNames are the apps status reports when none is named: every app the key opens.
func appNames(client query.Client) ([]string, error) {
	apps, err := client.Apps(context.Background())
	if err != nil {
		return nil, fmt.Errorf("asking for your apps: %w", err)
	}
	if len(apps.Apps) == 0 {
		return nil, errNoApps
	}
	names := make([]string, len(apps.Apps))
	for i, app := range apps.Apps {
		names[i] = app.App
	}
	return names, nil
}

// awaitApp asks for the org's apps until one is new or has sent data since the first answer, and
// names it. That is the app being set up, whichever name it reports under.
func awaitApp(w world, client query.Client) (string, error) {
	var first map[string]time.Time
	for waited := time.Duration(0); ; waited += pollEvery {
		apps, err := client.Apps(context.Background())
		if err != nil {
			return "", fmt.Errorf("asking for your apps: %w", err)
		}
		seen := make(map[string]time.Time, len(apps.Apps))
		for _, app := range apps.Apps {
			if app.LastDataAt != nil {
				seen[app.App] = *app.LastDataAt
			}
		}
		if waited == 0 {
			first = seen
		} else {
			for _, app := range apps.Apps {
				if last, ok := seen[app.App]; ok && last.After(first[app.App]) {
					return app.App, nil
				}
			}
		}
		if waited >= waitLimit {
			return "", errNoNewData
		}
		w.sleep(pollEvery)
	}
}

// awaitData asks until the sources of the app differ from the first answer, which is the proof
// that something arrived after the command started. An app that is not there yet is an empty answer.
func awaitData(w world, client query.Client, app string) (query.Status, error) {
	var first []query.Signal
	for waited := time.Duration(0); ; waited += pollEvery {
		status, err := client.Status(context.Background(), app)
		var absent *query.NotFoundError
		if err != nil && !errors.As(err, &absent) {
			return query.Status{}, fmt.Errorf("asking for %s: %w", app, err)
		}
		switch {
		case waited == 0:
			first = status.Signals
		case err == nil && !reflect.DeepEqual(first, status.Signals):
			return status, nil
		case waited >= waitLimit:
			return query.Status{}, errNoNewData
		}
		w.sleep(pollEvery)
	}
}

func newClient(w world) (query.Client, error) {
	key, err := query.Key(w.getenv, w.store)
	if err != nil {
		return query.Client{}, fmt.Errorf("finding the key: %w", err)
	}
	base := w.getenv("PACKRAT_QUERY_URL")
	if base == "" {
		base = query.DefaultURL
	}
	return query.Client{BaseURL: base, Key: key, HTTP: w.http, Version: version, Agent: query.Agent(w.getenv), Heard: w.heard}, nil
}

// runSetup is the setup topic: detect and plan, which read a project before it sends anything.
func runSetup(w world, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) > 0 && args[0] == "detect":
		return runDetect(args[1:], stdout, stderr)
	case len(args) > 0 && args[0] == "plan":
		return runPlan(w, args[1:], stdout, stderr)
	default:
		return say(stderr, "packrat setup: specify a command: detect or plan. Run packrat help setup for details.\n", exitUsage)
	}
}

func runPlan(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", "", "")
	asJSON := fs.Bool("json", false, "")
	verbose := fs.Bool("verbose", false, "")
	fs.BoolVar(verbose, "v", false, "")
	paths, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return say(stdout, topLevelHelp(), exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitUsage)
	}
	if len(paths) > 1 {
		return say(stderr, "packrat setup plan: specify at most one path\n", exitUsage)
	}
	path := "."
	if len(paths) == 1 {
		path = paths[0]
	}
	result, err := detect.Run(path, rails.DetectOptions())
	if errors.Is(err, detect.ErrNotDirectory) {
		return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitUsage)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitFailed)
	}
	if !report.Planned(result) {
		return say(stderr, fmt.Sprintf("packrat setup plan: %s\n", report.NoPlan), exitFailed)
	}
	var status *query.Status
	if *app != "" {
		client, err := newClient(w)
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitUsage)
		}
		answer, err := client.Status(context.Background(), *app)
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitFailed)
		}
		status = &answer
	}
	if *asJSON {
		err = report.PlanJSONFor(stdout, result, status)
	} else {
		err = report.PlanFor(stdout, result, status, *verbose)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat setup plan: %v\n", err), exitFailed)
	}
	return exitOK
}

func runDetect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	probe := fs.Bool("host", false, "")
	verbose := fs.Bool("verbose", false, "")
	fs.BoolVar(verbose, "v", false, "")
	paths, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return say(stdout, topLevelHelp(), exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat setup detect: %v\n", err), exitUsage)
	}
	if len(paths) > 1 {
		return say(stderr, "packrat setup detect: specify at most one path\n", exitUsage)
	}
	path := "."
	if len(paths) == 1 {
		path = paths[0]
	}
	opts := rails.DetectOptions()
	if *probe {
		opts.Host = host.Default().Probe
	}
	result, err := detect.Run(path, opts)
	if errors.Is(err, detect.ErrNotDirectory) {
		return say(stderr, fmt.Sprintf("packrat setup detect: %v\n", err), exitUsage)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat setup detect: %v\n", err), exitFailed)
	}
	write := report.Text
	switch {
	case *asJSON:
		write = report.JSON
	case *verbose:
		write = report.Verbose
	}
	if err := write(stdout, result); err != nil {
		return say(stderr, fmt.Sprintf("packrat setup detect: %v\n", err), exitFailed)
	}
	return exitOK
}

// parseInterleaved accepts flags before and after the path. The flag package alone stops at the
// first argument that is not a flag.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, fmt.Errorf("parsing flags: %w", err)
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func say(w io.Writer, msg string, code int) int {
	if _, err := io.WriteString(w, msg); err != nil && code == exitOK {
		return exitFailed
	}
	return code
}
