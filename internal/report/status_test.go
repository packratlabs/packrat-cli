package report_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/query"
	"github.com/packratlabs/packrat-cli/internal/report"
)

// Each case is an answer of GET /status in testdata/status, and the table it must give beside it.
// UPDATE=1 rewrites the tables.
func TestStatus(t *testing.T) {
	tests := []struct {
		name    string
		sending bool
	}{
		{name: "one-source", sending: true},
		{name: "many-sources", sending: true},
		{name: "kept-source-quiet", sending: false},
		{name: "nothing-yet", sending: false},
		{name: "stopped", sending: false},
		{name: "claimed-not-seen", sending: true},
		{name: "packrat-rails", sending: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := readStatus(t, tc.name)
			var out bytes.Buffer
			if err := report.Status(&out, status); err != nil {
				t.Fatalf("writing: %v", err)
			}
			golden := filepath.Join("testdata", "status", tc.name+".txt")
			if os.Getenv("UPDATE") != "" {
				if err := os.WriteFile(golden, out.Bytes(), 0o600); err != nil {
					t.Fatalf("updating %s: %v", golden, err)
				}
			}
			want, err := os.ReadFile(golden) // #nosec G304 -- a fixture of this test
			if err != nil {
				t.Fatalf("reading %s: %v", golden, err)
			}
			if out.String() != string(want) {
				t.Errorf("table differs from %s:\n%s", golden, out.String())
			}
			if got := report.Sending(status); got != tc.sending {
				t.Errorf("Sending is %v, want %v", got, tc.sending)
			}
		})
	}
}

func TestApps(t *testing.T) {
	now := time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC)
	recent, old := now.Add(-14*time.Second), now.Add(-3*time.Hour)
	tests := []struct {
		name string
		apps []query.App
		want string
	}{
		{name: "none", apps: []query.App{}, want: report.NoApps + "\n"},
		{name: "aligned, and one with no data", apps: []query.App{
			{App: "shop/production", LastDataAt: &recent},
			{App: "shop/dev", LastDataAt: &old},
			{App: "rails/production"},
		}, want: "APP               LAST DATA\nshop/production   now\nshop/dev          3 hours ago\nrails/production  none\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := report.Apps(&out, query.Apps{Now: now, Apps: tc.apps}); err != nil {
				t.Fatalf("writing: %v", err)
			}
			if out.String() != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", out.String(), tc.want)
			}
		})
	}
}

func readStatus(t *testing.T, name string) query.Status {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "status", name+".json")) // #nosec G304 -- a fixture of this test
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var status query.Status
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("the fixture is not JSON: %v", err)
	}
	return status
}
