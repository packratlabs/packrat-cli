package detect_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/detect"
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

// TestCorpus compares the detection and the plan of each real repository with their expected JSON. A repository that
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
			result, err := detect.Run(repo.dir, rails.DetectOptions())
			if err != nil {
				t.Fatalf("detection of %s: %v", repo.name, err)
			}
			result.Path = ""
			expected := filepath.Join(corpus, "expected")
			// The facts and the plan are two documents, and a rule can break one and not the other.
			for _, m := range []mode{{"json", report.JSON}, {"plan.json", report.PlanJSON}} {
				file := repo.name + "." + m.name
				var got bytes.Buffer
				if err := m.write(&got, result); err != nil {
					t.Fatalf("report of %s: %v", repo.name, err)
				}
				if os.Getenv("UPDATE") == "1" {
					if err := os.WriteFile(filepath.Join(expected, file), got.Bytes(), 0o600); err != nil {
						t.Fatalf("writing %s: %v", file, err)
					}
				}
				want, err := fs.ReadFile(os.DirFS(expected), file)
				if err != nil {
					t.Fatalf("reading %s: %v", file, err)
				}
				if !bytes.Equal(got.Bytes(), want) {
					t.Errorf("%s differs from corpus/expected/%s. got:\n%s", repo.name, file, got.Bytes())
				}
			}
		})
	}
}
