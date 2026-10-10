package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/report"
)

func TestRun(t *testing.T) {
	empty := filepath.Join("testdata", "empty")
	file := filepath.Join("testdata", "empty", ".gitkeep")
	agent := filepath.Join("testdata", "no-stack-agent-and-names")
	tests := []struct {
		name       string
		args       []string
		code       int
		stdout     string
		stderr     string
		lastStdout string
		absent     string
	}{
		{name: "empty directory", args: []string{"audit", empty}, code: 0, stdout: report.NoStack, lastStdout: report.Promise},
		{name: "flag after path", args: []string{"audit", empty, "--json"}, code: 0, stdout: `"schema": "packrat.audit/1"`},
		{name: "flag before path", args: []string{"audit", "--json", empty}, code: 0, stdout: `"schema": "packrat.audit/1"`},
		{name: "no host without the flag", args: []string{"audit", "--json", empty}, code: 0, stdout: `"host": null`},
		{name: "host flag", args: []string{"audit", "--host", empty}, code: 0, stdout: report.HostHeading, lastStdout: report.Promise},
		{name: "host flag with JSON", args: []string{"audit", "--host", "--json", empty}, code: 0, stdout: `"trace_listener":`},
		{name: "short table has no evidence", args: []string{"audit", agent}, code: 0, stdout: report.Hint, absent: "docker-compose.yml"},
		{name: "verbose", args: []string{"audit", "--verbose", agent}, code: 0, stdout: "docker-compose.yml:3", absent: report.Hint, lastStdout: report.Promise},
		{name: "verbose, short flag after path", args: []string{"audit", agent, "-v"}, code: 0, stdout: report.FoundHeading},
		{name: "JSON ignores verbose", args: []string{"audit", "--json", "-v", agent}, code: 0, stdout: `"vendor": "OpenTelemetry Collector"`, absent: report.FoundHeading},
		{name: "help", args: []string{"--help"}, code: 0, stdout: "packrat audit [path]"},
		{name: "audit help", args: []string{"audit", "--help"}, code: 0, stdout: "packrat audit [path]"},
		{name: "no command", args: nil, code: 2, stderr: "Usage:"},
		{name: "unknown command", args: []string{"setup"}, code: 2, stderr: `unknown command "setup"`},
		{name: "unknown flag", args: []string{"audit", "--yaml"}, code: 2, stderr: "flag provided but not defined"},
		{name: "two paths", args: []string{"audit", empty, empty}, code: 2, stderr: "takes one path"},
		{name: "path is a file", args: []string{"audit", file}, code: 2, stderr: "not a directory"},
		{name: "path is missing", args: []string{"audit", filepath.Join("testdata", "absent")}, code: 2, stderr: "not a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout lacks %q:\n%s", tc.stdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
			}
			if tc.absent != "" && strings.Contains(stdout.String(), tc.absent) {
				t.Errorf("stdout must not hold %q:\n%s", tc.absent, stdout.String())
			}
			if tc.lastStdout != "" && !strings.HasSuffix(stdout.String(), tc.lastStdout+"\n") {
				t.Errorf("last line is not %q:\n%s", tc.lastStdout, stdout.String())
			}
			if tc.code != 0 && stdout.Len() != 0 {
				t.Errorf("stdout is not empty on failure:\n%s", stdout.String())
			}
		})
	}
}

func TestJSONEmptyHasNoNullLists(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"audit", "--json", filepath.Join("testdata", "empty")}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"sdks", "agents", "browser", "env_names", "signals", "notes"} {
		if string(doc[key]) != "[]" {
			t.Errorf("%s is %s, want []", key, doc[key])
		}
	}
}
