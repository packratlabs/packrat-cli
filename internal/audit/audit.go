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

// StackInfo is the framework the repository was recognised as.
type StackInfo struct {
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

// Host holds facts about the machine that ran the audit. They say nothing about production.
// EnvSet holds names only; the value of a variable is never kept.
type Host struct {
	AgentPaths    []string `json:"agent_paths"`
	TraceListener bool     `json:"trace_listener"`
	EnvSet        []string `json:"env_set"`
	Notes         []string `json:"notes"`
}

// Production reports whether the gem loads in production. A gem with no groups does.
func (f Finding) Production() bool {
	for _, group := range f.Groups {
		if group != "development" && group != "test" {
			return true
		}
	}
	return len(f.Groups) == 0
}

// Facts is what a Stack read from a repository.
type Facts struct {
	Stack    StackInfo
	SDKs     []Finding
	Agents   []Agent
	Browser  []Browser
	EnvNames []EnvName
	Signals  []Signal
	Notes    []string
}

// Stack reads one kind of repository. Rails is the only implementation.
type Stack interface {
	Detect(root *os.Root) (bool, Evidence)
	Read(root *os.Root) (Facts, error)
}

// Audit is everything one run found. A nil Stack means no supported stack.
type Audit struct {
	Schema   string     `json:"schema"`
	Path     string     `json:"path"`
	Stack    *StackInfo `json:"stack"`
	SDKs     []Finding  `json:"sdks"`
	Agents   []Agent    `json:"agents"`
	Browser  []Browser  `json:"browser"`
	EnvNames []EnvName  `json:"env_names"`
	Signals  []Signal   `json:"signals"`
	Host     *Host      `json:"host"`
	Notes    []string   `json:"notes"`
}

// Options are the switches of one run. Stacks are tried in order and the first match is read.
// NoStack reads what does not depend on a stack, for a repository that no stack detected.
// Host probes the machine; it is nil unless the user asked for the probe.
type Options struct {
	Host    func() Host
	Stacks  []Stack
	NoStack func(root *os.Root) Facts
}

// Run audits the directory at path.
func Run(path string, opts Options) (Audit, error) {
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
	result, err := read(root, opts)
	if closeErr := root.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("closing %s: %w", path, closeErr)
	}
	if err != nil {
		return Audit{}, err
	}
	result.Path = abs
	if opts.Host != nil {
		probed := opts.Host()
		result.Host = &probed
	}
	return result, nil
}

func read(root *os.Root, opts Options) (Audit, error) {
	result := Audit{Schema: Schema}
	for _, stack := range opts.Stacks {
		found, evidence := stack.Detect(root)
		if !found {
			continue
		}
		facts, err := stack.Read(root)
		if err != nil {
			return Audit{}, fmt.Errorf("reading the %s stack: %w", facts.Stack.Name, err)
		}
		facts.Stack.Evidence = []Evidence{evidence}
		result.Stack = &facts.Stack
		return result.with(facts), nil
	}
	if opts.NoStack != nil {
		return result.with(opts.NoStack(root)), nil
	}
	return result.with(Facts{}), nil
}

// with copies facts into the audit. Every list is made non-nil so that the JSON has [] and
// never null.
func (a Audit) with(facts Facts) Audit {
	a.SDKs = append([]Finding{}, facts.SDKs...)
	a.Agents = append([]Agent{}, facts.Agents...)
	a.Browser = append([]Browser{}, facts.Browser...)
	a.EnvNames = append([]EnvName{}, facts.EnvNames...)
	a.Signals = append([]Signal{}, facts.Signals...)
	a.Notes = append([]string{}, facts.Notes...)
	return a
}
