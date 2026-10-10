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
	maxFileSize = 2 << 20
	noLockNote  = "no Gemfile.lock; run bundle install for versions"
)

// Stack is the Rails implementation of audit.Stack.
type Stack struct{}

// Detect reports whether the repository is a Rails application. The lockfile decides when there
// is one; the Gemfile is asked only when there is not.
func (Stack) Detect(root *os.Root) (bool, audit.Evidence) {
	path, lock := gems(root)
	gem, found := lock.Gems["rails"]
	if !found || gem.Line == 0 {
		return false, audit.Evidence{}
	}
	return true, audit.Evidence{Path: path, Line: gem.Line, Text: gem.Text}
}

// Read collects the facts of a Rails repository.
func (Stack) Read(root *os.Root) (audit.Facts, error) {
	path, lock := gems(root)
	facts := audit.Facts{Stack: audit.StackInfo{Name: "rails"}}
	if path != lockName {
		facts.Notes = append(facts.Notes, noLockNote)
	}
	facts.Stack.Version = lock.Gems["rails"].Version
	facts.Stack.Ruby = rubyVersion(root, lock)
	facts.SDKs = findings(path, lock, readGemfile(root))
	return facts, nil
}

// gems returns the gems of the repository and the file they were read from.
func gems(root *os.Root) (string, Lockfile) {
	if data, ok := readFile(root, lockName); ok {
		return lockName, ParseLockfile(data)
	}
	lock := Lockfile{Gems: map[string]Gem{}}
	for name, declared := range readGemfile(root).Gems {
		lock.Gems[name] = Gem{Direct: true, Source: "gemfile", Line: declared.Line, Text: declared.Text}
	}
	return gemfileName, lock
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

// readFile returns nothing for a file that is missing, a symlink, over 2 MB or not openable. One
// unreadable file must not fail the audit.
func readFile(root *os.Root, name string) ([]byte, bool) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return nil, false
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return data, true
}

// lines splits a file and drops the carriage return of a file saved on Windows.
func lines(data []byte) []string {
	all := strings.Split(string(data), "\n")
	for i, line := range all {
		all[i] = strings.TrimRight(line, " \t\r")
	}
	return all
}
