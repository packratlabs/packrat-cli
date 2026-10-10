package rails

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedFromFixtures(f *testing.F, name string) {
	f.Helper()
	fixtures := os.DirFS(filepath.Join("..", "..", "testdata"))
	paths, err := fs.Glob(fixtures, "*/"+name)
	if err != nil || len(paths) == 0 {
		f.Fatalf("no %s fixtures to seed with: %v", name, err)
	}
	for _, path := range paths {
		data, err := fs.ReadFile(fixtures, path)
		if err != nil {
			f.Fatalf("reading seed %s: %v", path, err)
		}
		f.Add(data)
	}
}

func FuzzLockfile(f *testing.F) {
	seedFromFixtures(f, "Gemfile.lock")
	f.Add([]byte("GEM\n  specs:\n    a (\n    (1.0)\n     (\nDEPENDENCIES\n  !\n  (x)\nRUBY VERSION\n   ruby \n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		lock := ParseLockfile(data)
		for name, gem := range lock.Gems {
			if !validName(name) {
				t.Errorf("gem name %q is not a name", name)
			}
			if gem.Line < 0 || strings.Contains(gem.Version, "-") {
				t.Errorf("gem %q has line %d and version %q", name, gem.Line, gem.Version)
			}
		}
	})
}

func FuzzGemfile(f *testing.F) {
	seedFromFixtures(f, "Gemfile")
	f.Add([]byte("group\ngroup(\ngem \"\ngem 'a', groups: %\ngem 'b', group: [\ngem 'c', groups: %i[\nend\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		gemfile := ParseGemfile(data)
		for name, declared := range gemfile.Gems {
			if name == "" || declared.Line < 1 {
				t.Errorf("gem %q has line %d", name, declared.Line)
			}
		}
	})
}
