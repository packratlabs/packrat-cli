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
	"path/filepath"
	"strings"
	"time"

	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/report"
)

const (
	queueUsage     = "packrat queue: specify a command: list, next, done or log. Run packrat help queue for details.\n"
	queuePollEvery = time.Minute
	queueLogLines  = 40
)

func runQueue(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", w.getenv("PACKRAT_APP"), "")
	asJSON := fs.Bool("json", false, "")
	wait := fs.Bool("wait", false, "")
	ending := fs.String("ending", "", "")
	note := fs.String("note", "", "")
	fix := fs.String("fix", "", "")
	check := fs.String("check", "", "")
	file := fs.String("file", "", "")
	lines := fs.Int("n", queueLogLines, "")
	words, err := parseInterleaved(fs, args)
	if err != nil || len(words) == 0 {
		return say(stderr, queueUsage, exitUsage)
	}
	where := query.Where{App: *app, Repo: originRepo(w)}
	switch {
	case words[0] == "list" && len(words) == 1:
		return queueList(w, where, *asJSON, stdout, stderr)
	case words[0] == "next" && len(words) == 1:
		return queueNext(w, where, *wait, *asJSON, stdout, stderr)
	case words[0] == "done" && len(words) == 2:
		return queueDone(w, words[1], *app, query.Ending{Ending: *ending, Note: *note, Fix: *fix, Check: *check}, *file, *asJSON, stdout, stderr)
	case words[0] == "log" && len(words) == 1:
		return queueLog(w, *lines, stdout, stderr)
	default:
		return say(stderr, queueUsage, exitUsage)
	}
}

func queueList(w world, where query.Where, asJSON bool, stdout, stderr io.Writer) int {
	const name = "queue list"
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	queue, err := client.Queue(context.Background(), where)
	if err != nil {
		return fail(stderr, name, name+" --app %s", err)
	}
	if asJSON {
		err = report.QueueJSON(stdout, queue)
	} else {
		err = report.Queue(stdout, queue)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	return exitOK
}

// queueNext takes the next failed pull request. With wait it asks again each minute until there is one: the first
// answer must be good, so a wrong app or key is said at once, and a later failure is asked again.
func queueNext(w world, where query.Where, wait, asJSON bool, stdout, stderr io.Writer) int {
	const name = "queue next"
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	for first := true; ; first = false {
		next, err := client.Next(context.Background(), where)
		switch {
		case err != nil && (first || !wait):
			return fail(stderr, name, name+" --app %s", err)
		case err != nil:
			fmt.Fprintf(stderr, "packrat %s: %v. Asking again in a minute.\n", name, err) //nolint:errcheck // a note on the way
		case next.Item != nil:
			logLoop(w, stderr, fmt.Sprintf("took   #%d  %s", next.Item.Number, next.Item.Title))
			if asJSON {
				err = report.NextJSON(stdout, next)
			} else {
				err = report.Item(stdout, *next.Item, width(w))
			}
			if err != nil {
				return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
			}
			return exitOK
		case !wait && asJSON:
			if err := report.NextJSON(stdout, next); err != nil {
				return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
			}
			return exitOK
		case !wait:
			return say(stdout, "No failed pull request waits in "+next.App+".\n", exitOK)
		case first:
			fmt.Fprintf(stderr, "No failed pull request waits in %s. Asking again each minute until one does.\n", next.App) //nolint:errcheck // a note on the way
		}
		w.sleep(queuePollEvery)
	}
}

// queueDone reports how a loop ended a pull request, which is what excuses a check or adds one, and logs it.
func queueDone(w world, ref, app string, ending query.Ending, file string, asJSON bool, stdout, stderr io.Writer) int {
	const name = "queue done"
	number, where, ok := pullRef(w, ref, app)
	if !ok {
		return say(stderr, fmt.Sprintf("packrat %s: %q is not a pull request number or URL\n", name, ref), exitUsage)
	}
	ending.Ending = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(ending.Ending)), "-", "_")
	if _, known := report.Endings[ending.Ending]; !known {
		return say(stderr, "packrat "+name+": --ending is one of fix, fixed-already, excuse, replace, needs-person\n", exitUsage)
	}
	if strings.TrimSpace(ending.Note) == "" {
		return say(stderr, "packrat "+name+": --note says the cause and what you did, in two plain sentences\n", exitUsage)
	}
	if file != "" {
		var check []byte
		var err error
		if file == "-" {
			check, err = io.ReadAll(w.stdin)
		} else {
			check, err = os.ReadFile(file) // #nosec G304 -- the file the user named
		}
		if err != nil {
			return say(stderr, fmt.Sprintf("packrat %s: reading %s: %v\n", name, file, err), exitUsage)
		}
		if !json.Valid(check) || !bytes.HasPrefix(bytes.TrimSpace(check), []byte("{")) {
			return say(stderr, fmt.Sprintf("packrat %s: %s holds one check as JSON: {\"title\", \"claim\", \"sql\", \"pass_when\", \"fail_when\", \"min_seen\", \"window\", \"at_end\"}\n", name, file), exitUsage)
		}
		ending.NewCheck = json.RawMessage(check)
	}
	client, err := waiting(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	ended, err := client.End(context.Background(), number, where, ending)
	if err != nil {
		return fail(stderr, name, name+" "+ref+" --app %s", err)
	}
	line := fmt.Sprintf("ended  #%d  %s", ended.Pull, report.Endings[ended.Ending])
	if changed := report.Changed(ended); changed != "" {
		line += " · " + changed
	}
	logLoop(w, stderr, line, ended.Note)
	if asJSON {
		err = report.EndedJSON(stdout, ended)
	} else {
		err = report.Ended(stdout, ended)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	if ended.Warning != "" {
		return say(stderr, fmt.Sprintf("packrat %s: warning: %s\n", name, ended.Warning), exitOK)
	}
	return exitOK
}

func queueLog(w world, lines int, stdout, stderr io.Writer) int {
	const name = "queue log"
	path, err := loopLogPath(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitUsage)
	}
	text, err := os.ReadFile(path) // #nosec G304 -- the log this command writes
	if errors.Is(err, os.ErrNotExist) {
		return say(stdout, "A loop has logged nothing in this repository. The log is "+path+"\n", exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat %s: %v\n", name, err), exitFailed)
	}
	kept := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	if lines > 0 && len(kept) > lines {
		kept = kept[len(kept)-lines:]
	}
	return say(stdout, strings.Join(kept, "\n")+"\n", exitOK)
}

// loopLogPath is the log of what a loop took and how each ended: a file inside .git, beside the checks drafts, so
// it is never committed and the main checkout and the loop's worktree share it.
func loopLogPath(w world) (string, error) {
	if w.program == nil {
		return "", errors.New("run it in a git checkout: the log is kept inside .git")
	}
	common, err := w.program("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	common = strings.TrimSpace(common)
	if err != nil || common == "" {
		return "", errors.New("run it in a git checkout: the log is kept inside .git")
	}
	return filepath.Join(common, "packrat", "loop.log"), nil
}

// logLoop appends one entry: the time and what happened, then each further line under it. A log that cannot be
// written is said, and does not fail the command: Packrat has already made the change.
func logLoop(w world, stderr io.Writer, what string, more ...string) {
	at := time.Now()
	if w.now != nil {
		at = w.now()
	}
	entry := at.Format("2006-01-02 15:04") + "  " + what + "\n"
	for _, line := range more {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			entry += strings.Repeat(" ", 25) + line + "\n"
		}
	}
	path, err := loopLogPath(w)
	if err == nil {
		err = appendTo(path, entry)
	}
	if err != nil {
		fmt.Fprintf(stderr, "packrat queue: could not write the loop's log: %v\n", err) //nolint:errcheck // a note on the way
	}
}

func appendTo(path, text string) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- the log inside .git
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("closing %s: %w", path, closeErr)
		}
	}()
	if _, err := file.WriteString(text); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
