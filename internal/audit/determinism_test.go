package audit_test

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/rails"
	"github.com/packratlabs/packrat-cli/internal/report"
)

type mode struct {
	name  string
	write func(io.Writer, audit.Audit) error
}

func modes() []mode {
	return []mode{{"txt", report.Text}, {"json", report.JSON}}
}

func fixtures(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata"))
	if err != nil {
		t.Fatalf("listing fixtures: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no fixtures")
	}
	return names
}

// render blanks the path of the JSON, which is absolute and differs on every machine.
func render(t *testing.T, fixture string, m mode) []byte {
	t.Helper()
	result, err := audit.Run(filepath.Join("..", "..", "testdata", fixture), rails.AuditOptions())
	if err != nil {
		t.Fatalf("audit of %s: %v", fixture, err)
	}
	if m.name == "json" {
		result.Path = ""
	}
	var out bytes.Buffer
	if err := m.write(&out, result); err != nil {
		t.Fatalf("report of %s: %v", fixture, err)
	}
	return out.Bytes()
}

func TestDeterminism(t *testing.T) {
	for _, fixture := range fixtures(t) {
		for _, m := range modes() {
			t.Run(fixture+"/"+m.name, func(t *testing.T) {
				first := render(t, fixture, m)
				for range 20 {
					if again := render(t, fixture, m); !bytes.Equal(first, again) {
						t.Fatalf("two runs differ:\n%s\n---\n%s", first, again)
					}
				}
			})
		}
	}
}

// TestGolden compares each fixture with its expected output. UPDATE=1 rewrites the files; read
// the diff before you commit it.
func TestGolden(t *testing.T) {
	for _, fixture := range fixtures(t) {
		for _, m := range modes() {
			t.Run(fixture+"/"+m.name, func(t *testing.T) {
				got := render(t, fixture, m)
				path := filepath.Join("testdata", "golden", fixture+"."+m.name)
				if os.Getenv("UPDATE") == "1" {
					if err := os.WriteFile(path, got, 0o600); err != nil {
						t.Fatalf("writing %s: %v", path, err)
					}
				}
				want, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
				if err != nil {
					t.Fatalf("reading %s: %v", path, err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs. got:\n%s\nwant:\n%s", path, got, want)
				}
			})
		}
	}
}

func TestTableIsUnder100Columns(t *testing.T) {
	for _, fixture := range fixtures(t) {
		for line := range strings.SplitSeq(string(render(t, fixture, modes()[0])), "\n") {
			if len(line) >= 100 {
				t.Errorf("%s: line of %d columns: %s", fixture, len(line), line)
			}
		}
	}
}
