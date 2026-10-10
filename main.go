// Command packrat audits a repository's observability. It only reads.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/host"
	"github.com/packratlabs/packrat-cli/internal/rails"
	"github.com/packratlabs/packrat-cli/internal/report"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

const usage = `packrat reads a repository and says what observability it has.

Usage:
  packrat audit [path] [--verbose] [--json] [--host]

  path     directory to audit (default: the current directory)
  -v, --verbose  also print the reason for each line and everything that was found
  --json   print the JSON document, which always holds everything
  --host   also probe this machine for a Datadog Agent; it opens one local socket

audit is read-only. It writes no file and sends nothing. Without --host it opens no socket.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return say(stderr, usage, exitUsage)
	}
	switch args[0] {
	case "audit":
		return runAudit(args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		return say(stdout, usage, exitOK)
	default:
		return say(stderr, fmt.Sprintf("packrat: unknown command %q\n\n%s", args[0], usage), exitUsage)
	}
}

func runAudit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
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
		return say(stderr, fmt.Sprintf("packrat audit: %v\n", err), exitUsage)
	}
	if len(paths) > 1 {
		return say(stderr, "packrat audit: takes one path\n", exitUsage)
	}
	path := "."
	if len(paths) == 1 {
		path = paths[0]
	}
	opts := rails.AuditOptions()
	if *probe {
		opts.Host = host.Default().Probe
	}
	result, err := audit.Run(path, opts)
	if errors.Is(err, audit.ErrNotDirectory) {
		return say(stderr, fmt.Sprintf("packrat audit: %v\n", err), exitUsage)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat audit: %v\n", err), exitFailed)
	}
	write := report.Text
	switch {
	case *asJSON:
		write = report.JSON
	case *verbose:
		write = report.Verbose
	}
	if err := write(stdout, result); err != nil {
		return say(stderr, fmt.Sprintf("packrat audit: %v\n", err), exitFailed)
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
