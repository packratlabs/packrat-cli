// Package rails reads a Ruby on Rails repository.
package rails

import (
	"os"
	"sort"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

const (
	lockName    = "Gemfile.lock"
	gemfileName = "Gemfile"
	noLockNote  = "no Gemfile.lock; run bundle install for versions"
)

// Stack is the Rails implementation of audit.Stack.
type Stack struct{}

// Detect reports whether the repository is a Rails application. The lockfile decides when there
// is one; the Gemfile is asked only when there is not.
func (Stack) Detect(root *os.Root) (bool, audit.Evidence) {
	path, lock := gems(root)
	gem, found := railsGem(lock)
	if !found {
		return false, audit.Evidence{}
	}
	return true, audit.Evidence{Path: path, Line: gem.Line, Text: gem.Text}
}

// railsGem returns the gem that makes the repository a Rails application. Discourse does not
// depend on `rails`; it names railties and the other parts one by one.
func railsGem(lock Lockfile) (Gem, bool) {
	for _, name := range []string{"rails", "railties"} {
		if gem, found := lock.Gems[name]; found && gem.Line > 0 {
			return gem, true
		}
	}
	return Gem{}, false
}

// Read collects the facts of a Rails repository.
func (Stack) Read(root *os.Root) (audit.Facts, error) {
	path, lock := gems(root)
	facts := audit.Facts{Stack: audit.StackInfo{Name: "rails"}}
	if path != lockName {
		facts.Notes = append(facts.Notes, noLockNote)
	}
	rails, _ := railsGem(lock)
	facts.Stack.Version = rails.Version
	facts.Stack.Ruby = rubyVersion(root, lock)
	facts.SDKs = configure(root, findings(path, lock, readGemfile(root)))
	facts.Agents = agents(root)
	facts.Browser = browser(root)
	facts.EnvNames = envNames(root)
	facts.Signals = Routes(facts)
	return facts, nil
}

// NoStack reads what does not depend on Rails: env names and agents.
func NoStack(root *os.Root) audit.Facts {
	return audit.Facts{Agents: agents(root), EnvNames: envNames(root)}
}

// AuditOptions are the options that make an audit read Rails repositories.
func AuditOptions() audit.Options {
	return audit.Options{Stacks: []audit.Stack{Stack{}}, NoStack: NoStack}
}

// gems returns the gems of the repository and the file they were read from.
func gems(root *os.Root) (string, Lockfile) {
	if data, ok := readFile(root, lockName); ok {
		return lockName, ParseLockfile(data)
	}
	lock := Lockfile{Gems: map[string]Gem{}}
	for name, declared := range readGemfile(root).Gems {
		lock.Gems[name] = Gem{Version: exactVersion(declared.Text), Direct: true, Source: "gemfile", Line: declared.Line, Text: declared.Text}
	}
	return gemfileName, lock
}

// exactVersion returns the version of a Gemfile line that pins one, such as `gem 'rails', '8.1.4'`.
// A constraint such as "~> 8.0" is not a version.
func exactVersion(line string) string {
	code, _, _ := strings.Cut(line, "#")
	parts := strings.Split(code, ",")
	if len(parts) != 2 {
		return ""
	}
	version := strings.Trim(strings.TrimSpace(parts[1]), `'"`)
	if version == "" || strings.Trim(version, "0123456789.") != "" {
		return ""
	}
	return version
}

func readGemfile(root *os.Root) Gemfile {
	data, _ := readFile(root, gemfileName)
	return ParseGemfile(data)
}

func rubyVersion(root *os.Root, lock Lockfile) string {
	if data, ok := readFile(root, ".ruby-version"); ok {
		first, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		if version := strings.TrimPrefix(strings.TrimSpace(first), "ruby-"); version != "" {
			return version
		}
	}
	return lock.Ruby
}

func findings(path string, lock Lockfile, gemfile Gemfile) []audit.Finding {
	found := []audit.Finding{}
	for name, gem := range lock.Gems {
		vendor, known := lookupVendor(name)
		if !known {
			continue
		}
		groups := gemfile.Gems[name].Groups
		if groups == nil {
			groups = []string{}
		}
		notes := []string{}
		if vendor.Note != "" {
			notes = append(notes, vendor.Note)
		}
		found = append(found, audit.Finding{
			Vendor:   vendor.Vendor,
			Gem:      name,
			Version:  gem.Version,
			Direct:   gem.Direct,
			Groups:   groups,
			Provides: vendor.Provides,
			Evidence: []audit.Evidence{{Path: path, Line: gem.Line, Text: gem.Text}},
			Notes:    notes,
		})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Vendor != found[j].Vendor {
			return found[i].Vendor < found[j].Vendor
		}
		return found[i].Gem < found[j].Gem
	})
	return found
}
