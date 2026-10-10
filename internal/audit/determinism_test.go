package audit_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/report"
)

func render(t *testing.T, fixture string, write func(io.Writer, audit.Audit) error) []byte {
	t.Helper()
	result, err := audit.Run(filepath.Join("..", "..", "testdata", fixture), audit.Options{})
	if err != nil {
		t.Fatalf("audit of %s: %v", fixture, err)
	}
	var out bytes.Buffer
	if err := write(&out, result); err != nil {
		t.Fatalf("report of %s: %v", fixture, err)
	}
	return out.Bytes()
}

func TestDeterminism(t *testing.T) {
	modes := []struct {
		name  string
		write func(io.Writer, audit.Audit) error
	}{
		{"text", report.Text},
		{"json", report.JSON},
	}
	for _, fixture := range []string{"empty"} {
		for _, mode := range modes {
			t.Run(fixture+"/"+mode.name, func(t *testing.T) {
				first := render(t, fixture, mode.write)
				second := render(t, fixture, mode.write)
				if len(first) == 0 {
					t.Fatal("no output")
				}
				if !bytes.Equal(first, second) {
					t.Errorf("two runs differ:\n%s\n---\n%s", first, second)
				}
			})
		}
	}
}
