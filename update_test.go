package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
	"github.com/packratlabs/packrat-cli/internal/query"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true},
		{"v1.0.0", "0.9.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.2.0", "dev", false},
		{"", "0.1.0", false},
		{"0.2.0-rc1", "0.1.0", false},
	}
	for _, tc := range tests {
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNotify(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = "0.1.0"
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		heard  query.Heard
		args   []string
		env    string
		last   string
		want   string
		absent bool
	}{
		{name: "a newer release", heard: query.Heard{Latest: "0.2.0"}, want: "\npackrat 0.2.0 is available (you have 0.1.0). Run: packrat update\n"},
		{name: "the newest already", heard: query.Heard{Latest: "0.1.0"}, absent: true},
		{name: "nothing heard", absent: true},
		{name: "told today", heard: query.Heard{Latest: "0.2.0"}, last: `{"version":"0.2.0","at":"2026-10-23T01:00:00Z"}`, absent: true},
		{name: "told yesterday", heard: query.Heard{Latest: "0.2.0"}, last: `{"version":"0.2.0","at":"2026-10-22T11:00:00Z"}`, want: "packrat 0.2.0 is available"},
		{name: "told today of an older release", heard: query.Heard{Latest: "0.3.0"}, last: `{"version":"0.2.0","at":"2026-10-23T11:00:00Z"}`, want: "packrat 0.3.0 is available"},
		{name: "turned off", heard: query.Heard{Latest: "0.2.0"}, env: "1", absent: true},
		{name: "too old, which the error already says", heard: query.Heard{Latest: "0.3.0", Minimum: "0.2.0"}, absent: true},
		{name: "not after update", heard: query.Heard{Latest: "0.2.0"}, args: []string{"update"}, absent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.last != "" {
				if err := os.WriteFile(filepath.Join(dir, noticeFile), []byte(tc.last), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			heard := tc.heard
			w := world{getenv: func(string) string { return tc.env }, store: auth.Store{Dir: dir}, now: func() time.Time { return now }, heard: &heard}
			var stderr bytes.Buffer
			args := tc.args
			if args == nil {
				args = []string{"apps"}
			}
			notify(w, args, &stderr)
			if tc.absent && stderr.Len() > 0 {
				t.Errorf("printed %q", stderr.String())
			}
			if !tc.absent && !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr is %q, want %q", stderr.String(), tc.want)
			}
			if !tc.absent {
				stderr.Reset()
				notify(w, []string{"apps"}, &stderr)
				if stderr.Len() > 0 {
					t.Errorf("told twice in a day: %q", stderr.String())
				}
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = "0.1.0"
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	files := tar.NewWriter(zipped)
	content := []byte("#!/bin/sh\necho new\n")
	_ = files.WriteHeader(&tar.Header{Name: "packrat", Mode: 0o755, Size: int64(len(content))})
	_, _ = files.Write(content)
	_ = files.Close()
	_ = zipped.Close()
	sum := sha256.Sum256(archive.Bytes())
	name := "packrat_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
		case "/download/v0.2.0/" + name:
			_, _ = w.Write(archive.Bytes())
		case "/download/v0.2.0/checksums.txt":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "packrat")
	if err := os.WriteFile(binary, []byte("old"), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	w := world{getenv: func(string) string { return "" }, store: auth.Store{Dir: t.TempDir()},
		releases:   query.Releases{API: server.URL + "/api", Download: server.URL + "/download", HTTP: server.Client()},
		executable: func() (string, error) { return binary, nil }}

	var stdout, stderr bytes.Buffer
	if code := runIn(w, []string{"update"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	got, _ := os.ReadFile(binary) // #nosec G304 -- the test's own file
	if string(got) != string(content) {
		t.Errorf("the binary is %q", got)
	}
	if info, _ := os.Stat(binary); info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the new binary is not executable: %v", info.Mode())
	}
	if want := "Updated packrat from 0.1.0 to 0.2.0 in "; !strings.HasPrefix(stdout.String(), want) {
		t.Errorf("stdout is %q", stdout.String())
	}

	version = "0.2.0"
	stdout.Reset()
	if code := runIn(w, []string{"update"}, &stdout, &stderr); code != exitOK || stdout.String() != "packrat 0.2.0 is the newest release.\n" {
		t.Errorf("exit %d, stdout %q", code, stdout.String())
	}
}
