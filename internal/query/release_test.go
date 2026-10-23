package query_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/query"
)

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	files := tar.NewWriter(zipped)
	if err := files.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// release serves one release, v0.2.0, the way GitHub does, with checksums.txt giving sum.
func release(t *testing.T, archive []byte, sum string) query.Releases {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("GitHub was sent an Authorization header")
		}
		switch r.URL.Path {
		case "/api/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
		case "/download/v0.2.0/packrat_linux_arm64.tar.gz":
			_, _ = w.Write(archive)
		case "/download/v0.2.0/checksums.txt":
			_, _ = w.Write([]byte(sum + "  packrat_linux_arm64.tar.gz\nabc  packrat_darwin_arm64.tar.gz\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return query.Releases{API: server.URL + "/api", Download: server.URL + "/download", HTTP: server.Client()}
}

func TestReleases(t *testing.T) {
	archive := tarGz(t, "packrat", []byte("new packrat"))
	sum := sha256.Sum256(archive)
	good := hex.EncodeToString(sum[:])
	ctx := context.Background()

	releases := release(t, archive, good)
	latest, err := releases.Latest(ctx)
	if err != nil || latest != "0.2.0" {
		t.Fatalf("latest is %q, %v; want 0.2.0", latest, err)
	}
	binary, err := releases.Binary(ctx, "0.2.0", "linux", "arm64")
	if err != nil || string(binary) != "new packrat" {
		t.Fatalf("binary is %q, %v", binary, err)
	}

	if _, err := release(t, archive, strings.Repeat("0", 64)).Binary(ctx, "0.2.0", "linux", "arm64"); !errors.Is(err, query.ErrChecksum) {
		t.Errorf("a wrong sum gives %v, want ErrChecksum", err)
	}
	if _, err := releases.Binary(ctx, "0.2.0", "freebsd", "arm64"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a missing archive gives %v, want a 404", err)
	}
	other := tarGz(t, "README.md", []byte("hello"))
	otherSum := sha256.Sum256(other)
	if _, err := release(t, other, hex.EncodeToString(otherSum[:])).Binary(ctx, "0.2.0", "linux", "arm64"); !errors.Is(err, query.ErrNoBinary) {
		t.Errorf("an archive without packrat gives %v, want ErrNoBinary", err)
	}
}

func TestHeard(t *testing.T) {
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.Header().Set("Packrat-CLI-Latest", "0.3.0")
		if r.URL.Path == "/status" {
			w.Header().Set("Packrat-CLI-Minimum", "0.2.0")
			w.WriteHeader(http.StatusUpgradeRequired)
			_, _ = w.Write([]byte(`{"type":"error","message":"packrat 0.1.0 is too old for this Packrat. Run packrat update."}`))
			return
		}
		_, _ = w.Write([]byte(`{"apps":[]}`))
	}))
	defer server.Close()
	heard := &query.Heard{}
	client := query.Client{BaseURL: server.URL, Key: testKey, HTTP: server.Client(), Version: "0.1.0", Heard: heard}

	if _, err := client.Apps(context.Background()); err != nil {
		t.Fatal(err)
	}
	if agent != "packrat-cli/0.1.0" || heard.Latest != "0.3.0" || heard.Minimum != "" {
		t.Errorf("agent %q, heard %+v", agent, *heard)
	}

	_, err := client.Status(context.Background(), "shop/production")
	var tooOld *query.TooOldError
	if !errors.As(err, &tooOld) || tooOld.Message != "packrat 0.1.0 is too old for this Packrat. Run packrat update." {
		t.Errorf("a 426 gives %v", err)
	}
	if heard.Minimum != "0.2.0" {
		t.Errorf("the minimum was not heard: %+v", *heard)
	}
}
