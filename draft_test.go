package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksDraft(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stdin  string
		branch string
		code   int
		stdout string
		stderr string
		draft  string
	}{
		{name: "the draft of a branch is kept in the shared .git", args: []string{"pr", "checks", "draft", "--file", "-"}, stdin: `[{"title":"The cart is faster"}]`, branch: "packrat/268",
			code: 0, stdout: "Validate it with packrat pr checks set --dry-run --file ", draft: `[{"title":"The cart is faster"}]`},
		{name: "a change production cannot show", args: []string{"pr", "checks", "draft", "--file", "-"}, stdin: `{"none":"Docs only."}`, branch: "docs", code: 0, draft: `{"none":"Docs only."}`},
		{name: "a draft is JSON", args: []string{"pr", "checks", "draft", "--file", "-"}, stdin: "title: x", branch: "docs", code: 2, stderr: "is not JSON"},
		{name: "a draft needs a file", args: []string{"pr", "checks", "draft"}, branch: "docs", code: 2, stderr: "specify --file"},
		{name: "a draft needs a branch", args: []string{"pr", "checks", "draft", "--file", "-"}, stdin: "[]", code: 2, stderr: "run it in a git checkout, on the branch of the pull request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			common := t.TempDir()
			w := world{getenv: func(string) string { return "" }, stdin: strings.NewReader(tc.stdin),
				program: func(_ string, args ...string) (string, error) {
					switch {
					case args[0] == "rev-parse":
						return common + "\n", nil
					case tc.branch == "":
						return "", errors.New("no branch")
					default:
						return tc.branch + "\n", nil
					}
				}}
			var stdout, stderr bytes.Buffer

			code := runIn(w, tc.args, &stdout, &stderr)

			if code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
			}
			kept, err := os.ReadFile(filepath.Join(common, "packrat", "checks", tc.branch+".json")) // #nosec G304 -- the test's own file
			if tc.draft == "" {
				if err == nil {
					t.Errorf("kept a draft: %s", kept)
				}
				return
			}
			if err != nil || string(kept) != tc.draft {
				t.Errorf("the draft holds %q (%v), want %q", kept, err, tc.draft)
			}
		})
	}
}
