package rails

import (
	"regexp"
	"strings"
	"unicode"
)

// ApplicationFile is where a Rails app names itself: the module around its Application class.
const ApplicationFile = "config/application.rb"

var applicationModule = regexp.MustCompile(`(?m)^\s*module\s+([A-Z][A-Za-z0-9_]*(?:::[A-Z][A-Za-z0-9_]*)*)\s*$`)

// AppName is the service name packrat-rails gives the app when nothing overrides it, read from config/application.rb:
// the module around Rails.application's class, underscored as Active Support does (`module PackratRailsBlank` is
// packrat_rails_blank). It is "" when the file names no module.
func AppName(application []byte) string {
	match := applicationModule.FindSubmatch(application)
	if match == nil {
		return ""
	}
	modules := strings.Split(string(match[1]), "::")
	return underscore(modules[len(modules)-1])
}

// underscore splits a camel-cased word before each capital that starts a word, as Active Support's underscore does:
// HTTPServer is http_server and App2Go is app2_go.
func underscore(word string) string {
	runes := []rune(word)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
