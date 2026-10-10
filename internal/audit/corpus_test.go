package audit_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/rails"
	"github.com/packratlabs/packrat-cli/internal/report"
)

type corpusRepo struct {
	name string
	dir  string
}

// corpusRepos reads corpus/repos.txt. A local: entry is a directory of this machine; every other
// entry is the clone that `make corpus` puts in corpus/src.
func corpusRepos(t *testing.T, corpus string) []corpusRepo {
	t.Helper()
	list, err := fs.ReadFile(os.DirFS(corpus), "repos.txt")
	if err != nil {
		t.Fatalf("reading repos.txt: %v", err)
	}
	var repos []corpusRepo
	for line := range strings.SplitSeq(string(list), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 3 {
			t.Fatalf("repos.txt: want `name url commit`, got %q", line)
		}
		dir := filepath.Join(corpus, "src", fields[0])
		if local, ok := strings.CutPrefix(fields[1], "local:"); ok {
			dir = local
			if rest, ok := strings.CutPrefix(local, "~/"); ok {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatalf("finding the home directory: %v", err)
				}
				dir = filepath.Join(home, filepath.FromSlash(rest))
			}
		}
		repos = append(repos, corpusRepo{name: fields[0], dir: dir})
	}
	return repos
}

// TestCorpus compares the audit of each real repository with its expected JSON. A repository that
// is not on this machine is skipped, so the test does nothing in CI. `make corpus` clones them.
// UPDATE=1 rewrites the expected files; read the diff against the repository's own files, and
// write what it taught in corpus/NOTES.md, before you commit it.
func TestCorpus(t *testing.T) {
	corpus := filepath.Join("..", "..", "corpus")
	for _, repo := range corpusRepos(t, corpus) {
		t.Run(repo.name, func(t *testing.T) {
			if info, err := os.Stat(repo.dir); err != nil || !info.IsDir() {
				t.Skipf("%s is not on this machine; run `make corpus`", repo.dir)
			}
			result, err := audit.Run(repo.dir, rails.AuditOptions())
			if err != nil {
				t.Fatalf("audit of %s: %v", repo.name, err)
			}
			result.Path = ""
			var got bytes.Buffer
			if err := report.JSON(&got, result); err != nil {
				t.Fatalf("report of %s: %v", repo.name, err)
			}
			expected := filepath.Join(corpus, "expected")
			if os.Getenv("UPDATE") == "1" {
				if err := os.WriteFile(filepath.Join(expected, repo.name+".json"), got.Bytes(), 0o600); err != nil {
					t.Fatalf("writing the expected file of %s: %v", repo.name, err)
				}
			}
			want, err := fs.ReadFile(os.DirFS(expected), repo.name+".json")
			if err != nil {
				t.Fatalf("reading the expected file of %s: %v", repo.name, err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("%s differs from corpus/expected/%s.json. got:\n%s", repo.name, repo.name, got.Bytes())
			}
		})
	}
}
