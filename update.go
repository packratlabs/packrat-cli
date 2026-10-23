package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// noticeEvery is how often the same release is announced.
const noticeEvery = 24 * time.Hour

const noticeFile = "update-notice.json"

type noticed struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// notify says, after the command's own output and at most once a day, that a newer release is out.
// It reads only what the API's answers said, so a command that asked nothing prints nothing.
func notify(w world, args []string, stderr io.Writer) {
	if w.heard == nil || w.now == nil || w.getenv("PACKRAT_NO_UPDATE_NOTICE") != "" || (len(args) > 0 && args[0] == "update") {
		return
	}
	latest := w.heard.Latest
	if !newer(latest, version) || newer(w.heard.Minimum, version) {
		return
	}
	path := filepath.Join(w.store.Dir, noticeFile)
	var last noticed
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &last) == nil && // #nosec G304 -- our own file in the config directory
		last.Version == latest && w.now().Sub(last.At) < noticeEvery {
		return
	}
	_, _ = fmt.Fprintf(stderr, "\npackrat %s is available (you have %s). Run: packrat update\n", latest, version)
	if data, err := json.Marshal(noticed{Version: latest, At: w.now()}); err == nil {
		_ = os.MkdirAll(w.store.Dir, 0o700)
		_ = os.WriteFile(path, data, 0o600)
	}
}

// newer is whether a is a later MAJOR.MINOR.PATCH than b. A version that is not one, such as a local
// build's dev, is never newer and never older.
func newer(a, b string) bool {
	x, okA := parseVersion(a)
	y, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func runUpdate(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	extra, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		text, _ := helpFor([]string{"update"})
		return say(stdout, text, exitOK)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat update: %v\n", err), exitUsage)
	}
	if len(extra) > 0 {
		return say(stderr, "packrat update: this command takes no arguments\n", exitUsage)
	}
	ctx := context.Background()
	latest, err := w.releases.Latest(ctx)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat update: finding the newest release: %v\n", err), exitFailed)
	}
	if !newer(latest, version) && version != "dev" {
		return say(stdout, fmt.Sprintf("packrat %s is the newest release.\n", version), exitOK)
	}
	path, err := w.executable()
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat update: finding this packrat: %v\n", err), exitFailed)
	}
	binary, err := w.releases.Binary(ctx, latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat update: %v\n", err), exitFailed)
	}
	if err := replace(path, binary); err != nil {
		return say(stderr, fmt.Sprintf("packrat update: %v\n", err), exitFailed)
	}
	return say(stdout, fmt.Sprintf("Updated packrat from %s to %s in %s.\n", version, latest, path), exitOK)
}

// replace swaps the binary at path for binary. The new file is written beside it and renamed over
// it, so a failure leaves the old one working. Windows can't replace a running program, but it can
// rename one, so there the old one is moved aside first.
func replace(path string, binary []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".packrat-update-*")
	if err != nil {
		return fmt.Errorf("can't write to %s: %w", dir, err)
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if _, err := file.Write(binary); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing the new packrat: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("writing the new packrat: %w", err)
	}
	if err := os.Chmod(temp, 0o755); err != nil { // #nosec G302 -- an executable
		return fmt.Errorf("making the new packrat executable: %w", err)
	}
	if runtime.GOOS == "windows" {
		old := path + ".old"
		_ = os.Remove(old)
		if err := os.Rename(path, old); err != nil {
			return fmt.Errorf("moving the old packrat aside: %w", err)
		}
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}
