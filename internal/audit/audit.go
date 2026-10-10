// Package audit holds the result of an audit and the entry point that produces it.
package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Schema names the version of the JSON document.
const Schema = "packrat.audit/1"

// ErrNotDirectory reports a path that does not exist or is not a directory.
var ErrNotDirectory = errors.New("not a directory")

// SignalName is one of the four signals the audit routes.
type SignalName string

// The signals of version 1. Metrics are not one of them.
const (
	Exceptions SignalName = "exceptions"
	Logs       SignalName = "logs"
	Traces     SignalName = "traces"
	Replays    SignalName = "replays"
)

// State says what setup would have to do for a signal.
type State string

// The states a signal can be in.
const (
	StateRoute       State = "route"
	StateAlready     State = "already"
	StateLater       State = "later"
	StateUnsupported State = "unsupported"
)

// Evidence is the file and line a fact was read from. Path is relative to the audited root and
// uses forward slashes on every OS.
type Evidence struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Stack is the framework the repository was recognised as.
type Stack struct {
	Name     string     `json:"name"`
	Version  string     `json:"version"`
	Ruby     string     `json:"ruby"`
	Evidence []Evidence `json:"evidence"`
}

// Finding is an observability library found in the repository.
type Finding struct {
	Vendor     string     `json:"vendor"`
	Gem        string     `json:"gem"`
	Version    string     `json:"version"`
	Direct     bool       `json:"direct"`
	Groups     []string   `json:"groups"`
	Configured bool       `json:"configured"`
	Provides   []string   `json:"provides"`
	Evidence   []Evidence `json:"evidence"`
	Notes      []string   `json:"notes"`
}

// Agent is an agent or collector whose configuration is in the repository.
type Agent struct {
	Vendor   string     `json:"vendor"`
	Evidence []Evidence `json:"evidence"`
}

// Browser is a browser SDK found in the repository.
type Browser struct {
	Vendor   string     `json:"vendor"`
	Package  string     `json:"package"`
	Replays  bool       `json:"replays"`
	Evidence []Evidence `json:"evidence"`
}

// EnvName is an environment variable name and the files that name it. The value is never read.
type EnvName struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
}

// Signal is the route the audit picks for one signal.
type Signal struct {
	Name    SignalName `json:"name"`
	Route   string     `json:"route"`
	State   State      `json:"state"`
	Reason  string     `json:"reason"`
	Blocked []string   `json:"blocked"`
}

// Host holds facts about the machine that ran the audit.
type Host struct {
	Notes []string `json:"notes"`
}

// Audit is everything one run found. A nil Stack means no supported stack.
type Audit struct {
	Schema   string    `json:"schema"`
	Path     string    `json:"path"`
	Stack    *Stack    `json:"stack"`
	SDKs     []Finding `json:"sdks"`
	Agents   []Agent   `json:"agents"`
	Browser  []Browser `json:"browser"`
	EnvNames []EnvName `json:"env_names"`
	Signals  []Signal  `json:"signals"`
	Host     *Host     `json:"host"`
	Notes    []string  `json:"notes"`
}

// Options are the switches of one run.
type Options struct {
	Host bool
}

// Run audits the directory at path. It reads nothing yet and returns an empty Audit.
func Run(path string, _ Options) (Audit, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Audit{}, fmt.Errorf("resolving %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return Audit{}, fmt.Errorf("%s: %w", path, ErrNotDirectory)
	}
	if err != nil {
		return Audit{}, fmt.Errorf("reading %s: %w", path, err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return Audit{}, fmt.Errorf("opening %s: %w", path, err)
	}
	if err := root.Close(); err != nil {
		return Audit{}, fmt.Errorf("closing %s: %w", path, err)
	}
	return Audit{
		Schema:   Schema,
		Path:     abs,
		SDKs:     []Finding{},
		Agents:   []Agent{},
		Browser:  []Browser{},
		EnvNames: []EnvName{},
		Signals:  []Signal{},
		Notes:    []string{},
	}, nil
}
