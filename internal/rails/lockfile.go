package rails

import "strings"

// Gem is one gem of a lockfile. Line is 0 for a gem that DEPENDENCIES names and no specs block
// lists, which happens for a gem of another platform.
type Gem struct {
	Version string
	Direct  bool
	Source  string
	Line    int
	Text    string
}

// Lockfile is the part of Gemfile.lock the audit uses.
type Lockfile struct {
	Gems map[string]Gem
	Ruby string
}

// ParseLockfile reads a Gemfile.lock. It accepts any input and never fails: a line it does not
// know is skipped. Sections it does not know (CHECKSUMS, PLATFORMS) are skipped whole, which
// matters because a CHECKSUMS line has the shape of a DEPENDENCIES line.
func ParseLockfile(data []byte) Lockfile {
	lock := Lockfile{Gems: map[string]Gem{}}
	section, inSpecs := "", false
	for i, line := range lines(data) {
		body := strings.TrimLeft(line, " ")
		indent := len(line) - len(body)
		switch {
		case body == "":
		case indent == 0:
			section, inSpecs = body, false
		case isSource(section) && indent == 2:
			inSpecs = body == "specs:"
		case isSource(section) && indent == 4 && inSpecs:
			lock.addSpec(strings.ToLower(section), body, i+1)
		case section == "DEPENDENCIES" && indent == 2:
			lock.addDirect(body)
		case section == "RUBY VERSION" && lock.Ruby == "":
			lock.Ruby = leadingVersion(strings.TrimPrefix(body, "ruby "))
		}
	}
	return lock
}

func isSource(section string) bool {
	return section == "GEM" || section == "GIT" || section == "PATH"
}

// addSpec keeps the first entry of a gem. Later entries are the same version built for one
// platform, `nokogiri (1.18.9-arm64-darwin)`.
func (l Lockfile) addSpec(source, body string, line int) {
	name, rest, found := strings.Cut(body, " (")
	if !found || !validName(name) {
		return
	}
	if existing, seen := l.Gems[name]; seen && existing.Line != 0 {
		return
	}
	version, _, _ := strings.Cut(strings.TrimSuffix(rest, ")"), "-")
	l.Gems[name] = Gem{Version: version, Direct: l.Gems[name].Direct, Source: source, Line: line, Text: body}
}

func (l Lockfile) addDirect(body string) {
	name, _, _ := strings.Cut(strings.TrimSuffix(body, "!"), " (")
	if !validName(name) {
		return
	}
	gem := l.Gems[name]
	gem.Direct = true
	l.Gems[name] = gem
}

func validName(name string) bool {
	return name != "" && !strings.ContainsAny(name, " \t()")
}

// leadingVersion drops the patch level of `3.1.6p260`.
func leadingVersion(s string) string {
	end := 0
	for end < len(s) && (s[end] == '.' || (s[end] >= '0' && s[end] <= '9')) {
		end++
	}
	return s[:end]
}
