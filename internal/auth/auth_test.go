package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

type memory map[string]string

func (m memory) Get(account string) (string, error) {
	token, found := m[account]
	if !found {
		return "", errors.New("no token")
	}
	return token, nil
}
func (m memory) Set(account, token string) error { m[account] = token; return nil }
func (m memory) Delete(account string) error     { delete(m, account); return nil }

func TestStore(t *testing.T) {
	tests := []struct {
		name    string
		secrets auth.Secrets
		inFile  bool
	}{
		{name: "token in the file", inFile: true},
		{name: "token apart from the file", secrets: memory{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := auth.Store{Dir: filepath.Join(t.TempDir(), "packrat"), Secrets: tc.secrets}
			if _, err := store.Current(); !errors.Is(err, auth.ErrNotSignedIn) {
				t.Fatalf("an empty store answers %v, want ErrNotSignedIn", err)
			}

			for _, key := range []string{"key_1", "key_2"} {
				if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: key, Token: "packrat_api_" + key}); err != nil {
					t.Fatalf("saving %s: %v", key, err)
				}
			}
			current, err := store.Current()
			if err != nil || current.Key != "key_2" || current.Token != "packrat_api_key_2" {
				t.Fatalf("current is %+v (%v), want the last sign-in with its token", current, err)
			}

			data, err := os.ReadFile(filepath.Join(store.Dir, "credentials.json"))
			if err != nil {
				t.Fatalf("reading the file: %v", err)
			}
			if strings.Contains(string(data), "packrat_api_") != tc.inFile {
				t.Errorf("token in the file is %v, want %v:\n%s", !tc.inFile, tc.inFile, data)
			}
			// Windows keeps no Unix mode; the file's ACL is the user profile's.
			info, err := os.Stat(filepath.Join(store.Dir, "credentials.json"))
			if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
				t.Errorf("the file's mode is %v (%v), want 0600", info.Mode().Perm(), err)
			}

			if err := store.Remove(); err != nil {
				t.Fatalf("removing: %v", err)
			}
			if _, err := store.Current(); !errors.Is(err, auth.ErrNotSignedIn) {
				t.Errorf("after removing, current answers %v, want ErrNotSignedIn", err)
			}
			if err := store.Remove(); err != nil {
				t.Errorf("removing twice: %v", err)
			}
		})
	}
}

func TestDir(t *testing.T) {
	got, err := auth.Dir(func(string) string { return "/config" })
	if err != nil || got != filepath.Join("/config", "packrat") {
		t.Errorf("Dir is %q (%v), want /config/packrat", got, err)
	}
}
