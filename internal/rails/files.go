package rails

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

const (
	maxFileSize = 2 << 20
	maxTextSize = 200
)

// source is one file, split in lines. Paths are relative to the root with forward slashes.
type source struct {
	path  string
	lines []string
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

// readSources skips a binary file, which `bin/*` can hold.
func readSources(root *os.Root, paths []string) []source {
	var sources []source
	for _, name := range paths {
		data, ok := readFile(root, name)
		if !ok || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		sources = append(sources, source{path: name, lines: lines(data)})
	}
	return sources
}

// lines splits a file and drops the carriage return of a file saved on Windows.
func lines(data []byte) []string {
	all := strings.Split(string(data), "\n")
	for i, line := range all {
		all[i] = strings.TrimRight(line, " \t\r")
	}
	return all
}

// list returns the regular files directly in dir whose name passes match, in order.
func list(root *os.Root, dir string, match func(name string) bool) []string {
	entries, err := fs.ReadDir(root.FS(), dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && match(entry.Name()) {
			found = append(found, path.Join(dir, entry.Name()))
		}
	}
	return found
}

// walk returns the regular files under dir whose name passes match, in order. It does not follow
// a symlink, because a directory entry that is a symlink is neither a directory nor regular.
func walk(root *os.Root, dir string, match func(name string) bool) []string {
	var found []string
	err := fs.WalkDir(root.FS(), dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || (entry.IsDir() && skipped(name)) {
			return fs.SkipDir
		}
		if entry.Type().IsRegular() && match(entry.Name()) {
			found = append(found, name)
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return found
}

func skipped(dir string) bool {
	switch path.Base(dir) {
	case ".git", "node_modules":
		return true
	}
	switch dir {
	case "vendor/bundle", "tmp", "log", "public/assets", "public/packs", "storage":
		return true
	}
	return false
}

func anyName(string) bool { return true }

func hasExtension(extensions ...string) func(string) bool {
	return func(name string) bool {
		for _, extension := range extensions {
			if strings.HasSuffix(name, extension) {
				return true
			}
		}
		return false
	}
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func isComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//")
}

// clip keeps evidence text short. A minified file can be one line of 2 MB.
func clip(line string) string {
	text := strings.TrimSpace(line)
	if len(text) <= maxTextSize {
		return text
	}
	end := maxTextSize
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

func sortEvidence(evidence []audit.Evidence) {
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Path != evidence[j].Path {
			return evidence[i].Path < evidence[j].Path
		}
		return evidence[i].Line < evidence[j].Line
	})
}
