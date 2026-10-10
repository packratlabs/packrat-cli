package audit_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/rails"
)

const canary = "CANARY_VALUE_DO_NOT_PRINT_7f3a"

// TestCanary is the proof that the audit never prints a value. Every file of the canary fixture
// that can hold a secret holds the canary string, next to names and patterns the audit does
// report. Do not weaken this test to make a change pass.
func TestCanary(t *testing.T) {
	planted := canaryFiles(t)
	for _, required := range []string{".env", ".env.example", ".env_sample", "app.json", "docker-compose.yml", "config/deploy.yml"} {
		if !planted[required] {
			t.Errorf("the fixture file %s no longer holds the canary", required)
		}
	}
	for _, m := range modes() {
		if out := render(t, "canary", m); bytes.Contains(out, []byte(canary)) {
			t.Errorf("the %s output holds the canary:\n%s", m.name, out)
		}
	}
	result, err := audit.Run(filepath.Join("..", "..", "testdata", "canary"), rails.AuditOptions())
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range allEvidence(result) {
		if rails.NamesOnly(e.Path) {
			checked++
			if e.Text != "" {
				t.Errorf("evidence %s:%d of a names-only file has text %q", e.Path, e.Line, e.Text)
			}
		}
	}
	if checked == 0 || len(result.EnvNames) == 0 {
		t.Errorf("the test proves nothing: %d names-only evidence, %d env names", checked, len(result.EnvNames))
	}
	for _, name := range result.EnvNames {
		for _, file := range name.Files {
			if !planted[file] {
				t.Errorf("%s was read from %s, which holds no canary", name.Name, file)
			}
		}
	}
}

func canaryFiles(t *testing.T) map[string]bool {
	t.Helper()
	fixture := os.DirFS(filepath.Join("..", "..", "testdata", "canary"))
	planted := map[string]bool{}
	err := fs.WalkDir(fixture, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", name, err)
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fixture, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		planted[name] = bytes.Contains(data, []byte(canary))
		return nil
	})
	if err != nil {
		t.Fatalf("reading the canary fixture: %v", err)
	}
	return planted
}

func allEvidence(a audit.Audit) []audit.Evidence {
	var all []audit.Evidence
	if a.Stack != nil {
		all = append(all, a.Stack.Evidence...)
	}
	for _, sdk := range a.SDKs {
		all = append(all, sdk.Evidence...)
	}
	for _, agent := range a.Agents {
		all = append(all, agent.Evidence...)
	}
	for _, browser := range a.Browser {
		all = append(all, browser.Evidence...)
	}
	return all
}
