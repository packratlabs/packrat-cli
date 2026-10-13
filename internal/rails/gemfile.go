package rails

import (
	"sort"
	"strings"
)

// Declared is one gem of a Gemfile. No groups means the gem loads in production.
type Declared struct {
	Groups []string
	Line   int
	Text   string
}

// Gemfile is the gems a Gemfile names and the groups they are in.
type Gemfile struct {
	Gems map[string]Declared
}

type openGroup struct {
	indent string
	names  []string
}

// ParseGemfile reads groups line by line. It is approximate: a Gemfile is Ruby, and this does
// not run Ruby. It knows `group ... do` closed by an `end` at the same indentation, and
// `group:` or `groups:` on a gem line. Anything else (a conditional, a loop, a gemspec, an
// eval_gemfile) is not understood, and a gem it cannot place gets no groups, which the detection
// treats as production. The error is in the safe direction: a gem is reported, not hidden.
func ParseGemfile(data []byte) Gemfile {
	gemfile := Gemfile{Gems: map[string]Declared{}}
	production := map[string]bool{}
	var open []openGroup
	for i, raw := range lines(data) {
		line, _, _ := strings.Cut(raw, " #")
		body := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(body)]
		switch {
		case hasKeyword(body, "group"):
			head, _, _ := strings.Cut(body, " do")
			open = append(open, openGroup{indent: indent, names: symbols(head[len("group"):])})
		case body == "end" && len(open) > 0 && open[len(open)-1].indent == indent:
			open = open[:len(open)-1]
		case hasKeyword(body, "gem"):
			name, rest := quoted(body[len("gem"):])
			if name == "" {
				continue
			}
			groups := inlineGroups(rest)
			for _, group := range open {
				groups = append(groups, group.names...)
			}
			production[name] = production[name] || len(groups) == 0
			gemfile.add(name, groups, i+1, body)
		}
	}
	for name := range production {
		if production[name] {
			declared := gemfile.Gems[name]
			declared.Groups = nil
			gemfile.Gems[name] = declared
		}
	}
	return gemfile
}

func (g Gemfile) add(name string, groups []string, line int, text string) {
	declared, seen := g.Gems[name]
	if !seen {
		declared = Declared{Line: line, Text: text}
	}
	declared.Groups = uniqueSorted(append(declared.Groups, groups...))
	g.Gems[name] = declared
}

// hasKeyword reports whether body is a call of word: `gem "x"` and `gem("x")`, not `gemspec`.
func hasKeyword(body, word string) bool {
	rest, found := strings.CutPrefix(body, word)
	return found && rest != "" && strings.ContainsRune(" \t(", rune(rest[0]))
}

// quoted returns the first quoted string of s and what follows it.
func quoted(s string) (string, string) {
	start := strings.IndexAny(s, `"'`)
	if start < 0 {
		return "", ""
	}
	length := strings.IndexByte(s[start+1:], s[start])
	if length < 0 {
		return "", ""
	}
	return s[start+1 : start+1+length], s[start+2+length:]
}

// inlineGroups reads `group: :test`, `groups: [:a, :b]`, `groups: %i[a b]` and the `:group =>`
// form from the options of a gem line.
func inlineGroups(options string) []string {
	for _, key := range []string{"groups:", "group:", ":groups =>", ":group =>"} {
		_, value, found := strings.Cut(options, key)
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		if list, isWords := strings.CutPrefix(value, "%"); isWords && len(list) > 1 {
			words, _, _ := strings.Cut(list[2:], "]")
			return strings.Fields(words)
		}
		if list, isArray := strings.CutPrefix(value, "["); isArray {
			inside, _, _ := strings.Cut(list, "]")
			return symbols(inside)
		}
		first, _, _ := strings.Cut(value, ",")
		return symbols(first)
	}
	return nil
}

// symbols returns the `:name` symbols and quoted strings of s. `optional: true` has neither.
func symbols(s string) []string {
	var names []string
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == ':' && i+1 < len(s) && isWordByte(s[i+1]):
			end := i + 1
			for end < len(s) && isWordByte(s[end]) {
				end++
			}
			names = append(names, s[i+1:end])
			i = end - 1
		case s[i] == '"' || s[i] == '\'':
			name, _ := quoted(s[i:])
			if name == "" {
				return names
			}
			names = append(names, name)
			i += len(name) + 1
		}
	}
	return names
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func uniqueSorted(names []string) []string {
	sort.Strings(names)
	var unique []string
	for _, name := range names {
		if len(unique) == 0 || unique[len(unique)-1] != name {
			unique = append(unique, name)
		}
	}
	return unique
}
