package query_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/query"
)

const testKey = "packrat_api_canary"

func TestKey(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		file    string
		want    string
		wantErr error
	}{
		{name: "the environment wins", env: "from-env", file: "PACKRAT_API_KEY=from-file\n", want: "from-env"},
		{name: "the env file", file: "OTHER=1\nPACKRAT_API_KEY=from-file\n", want: "from-file"},
		{name: "export and quotes", file: "export PACKRAT_API_KEY=\"from-file\"\n", want: "from-file"},
		{name: "another name with the same start", file: "PACKRAT_API_KEY_OLD=old\n", wantErr: query.ErrNoKey},
		{name: "a name with no value", file: "PACKRAT_API_KEY=\n", wantErr: query.ErrNoKey},
		{name: "no file", wantErr: query.ErrNoKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(tc.file), 0o600); err != nil {
					t.Fatalf("writing .env: %v", err)
				}
			}
			got, err := query.Key(func(string) string { return tc.env }, dir)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error is %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("key is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClient(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		body     string
		wantErr  string
		notFound bool
		refused  bool
		sources  int
	}{
		{name: "an answer", code: 200, body: `{"app":"shop/production","signals":[{"signal":"traces","sources":[{"source":"otel","name":"OpenTelemetry","kept":true}]},{"signal":"logs"}]}`, sources: 1},
		{name: "a refused key", code: 401, refused: true, wantErr: "refused"},
		{name: "an app that is not there", code: 404, body: `{"type":"error","message":"This key opens shop/production."}`, notFound: true, wantErr: "This key opens shop/production."},
		{name: "an API that has no such address", code: 404, wantErr: "has no /status (404). The API is older than this command"},
		{name: "a failure with no reason", code: 500, body: "<html>", wantErr: "answered 500: no reason given"},
		{name: "an answer that is not JSON", code: 200, body: "<html>", wantErr: "reading the answer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var asked, authorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked, authorization = r.URL.String(), r.Header.Get("Authorization")
				w.WriteHeader(tc.code)
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("answering: %v", err)
				}
			}))
			defer server.Close()
			client := query.Client{BaseURL: server.URL + "/", Key: testKey, HTTP: server.Client()}

			status, err := client.Status(context.Background(), "shop/production")

			if asked != "/status?app=shop%2Fproduction" || authorization != "Bearer "+testKey {
				t.Errorf("asked %q with %q", asked, authorization)
			}
			if tc.wantErr == "" && err != nil {
				t.Fatalf("error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error is %v, want one with %q", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), testKey) {
				t.Errorf("the error holds the key: %v", err)
			}
			var absent *query.NotFoundError
			if errors.As(err, &absent) != tc.notFound || errors.Is(err, query.ErrRefused) != tc.refused {
				t.Errorf("wrong kind of error: %v", err)
			}
			if tc.wantErr == "" {
				if len(status.Signals) != 2 || len(status.Signals[0].Sources) != tc.sources {
					t.Errorf("signals were not read: %+v", status.Signals)
				}
				if status.Signals[1].Sources == nil || status.Warnings == nil {
					t.Error("a missing list must be empty and not nil")
				}
			}
		})
	}
}
