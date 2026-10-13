// Command packrat detects a repository's observability and asks Packrat what it receives. It only reads.
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
	"time"

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

const usage = `packrat reads a repository and says what observability it has, and asks Packrat what it receives.

Usage:
  packrat detect [path] [--verbose] [--json] [--host]
  packrat apps [--json]
  packrat status --app service/env [--wait] [--json]

detect
  path     directory to read (default: the current directory)
  -v, --verbose  also print the reason for each line and everything that was found
  --json   print the JSON document, which always holds everything
  --host   also probe this machine for a Datadog Agent; it opens one local socket

detect is read-only. It writes no file and sends nothing. Without --host it opens no socket.

apps
  lists the apps that your API key opens, with the time of their last data

status
  --app    the app, as service/env; packrat apps lists them
  --wait   wait until new data arrives, for 5 minutes at most
  --json   print the answer of the API

apps and status ask query.packrat.sh with PACKRAT_API_KEY, from the environment or from .env in
the current directory. They send the key and the name of the app. They send nothing else.
`

const (
	pollEvery = 3 * time.Second
	waitLimit = 5 * time.Minute
	httpLimit = 30 * time.Second
)

// world is what a command reads from outside its arguments. Tests give their own.
type world struct {
	getenv func(string) string
	dir    string
	sleep  func(time.Duration)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runIn(world{getenv: os.Getenv, dir: ".", sleep: time.Sleep}, args, stdout, stderr)
}

func runIn(w world, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return say(stderr, usage, exitUsage)
	}
	switch args[0] {
	case "detect":
		return runDetect(args[1:], stdout, stderr)
	case "apps":
		return runApps(w, args[1:], stdout, stderr)
	case "status":
		return runStatus(w, args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		return say(stdout, usage, exitOK)
	default:
		return say(stderr, fmt.Sprintf("packrat: unknown command %q\n\n%s", args[0], usage), exitUsage)
	}
}

func runApps(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	extra, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return say(stdout, usage, exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat apps: %v\n", err), exitUsage)
	}
	if len(extra) > 0 {
		return say(stderr, "packrat apps: takes no argument\n", exitUsage)
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
		return say(stdout, usage, exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitUsage)
	}
	if len(extra) > 0 || *app == "" {
		return say(stderr, "packrat status: give an app with --app service/env. To list them: packrat apps\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat status: %v\n", err), exitUsage)
	}
	var status query.Status
	if *wait {
		if !*asJSON {
			if code := say(stdout, fmt.Sprintf("Waiting for data from %s. Start the app and load a page.\n\n", *app), exitOK); code != exitOK {
				return code
			}
		}
		status, err = awaitData(w, client, *app)
	} else {
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

var errNoNewData = errors.New("no new data arrived in 5 minutes")

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
	key, err := query.Key(w.getenv, w.dir)
	if err != nil {
		return query.Client{}, fmt.Errorf("finding the key: %w", err)
	}
	base := w.getenv("PACKRAT_QUERY_URL")
	if base == "" {
		base = query.DefaultURL
	}
	return query.Client{BaseURL: base, Key: key, HTTP: &http.Client{Timeout: httpLimit}}, nil
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
		return say(stdout, usage, exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat detect: %v\n", err), exitUsage)
	}
	if len(paths) > 1 {
		return say(stderr, "packrat detect: takes one path\n", exitUsage)
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
		return say(stderr, fmt.Sprintf("packrat detect: %v\n", err), exitUsage)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat detect: %v\n", err), exitFailed)
	}
	write := report.Text
	switch {
	case *asJSON:
		write = report.JSON
	case *verbose:
		write = report.Verbose
	}
	if err := write(stdout, result); err != nil {
		return say(stderr, fmt.Sprintf("packrat detect: %v\n", err), exitFailed)
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
