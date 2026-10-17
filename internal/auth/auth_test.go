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

func (m memory) Get(org string) (string, error) {
	token, found := m[org]
	if !found {
		return "", errors.New("no token")
	}
	return token, nil
}
func (m memory) Set(org, token string) error { m[org] = token; return nil }
func (m memory) Delete(org string) error     { delete(m, org); return nil }

func TestStore(t *testing.T) {
	tests := []struct {
		name    string
		secrets auth.Secrets
		inFile  bool
	}{
		{name: "tokens in the file", inFile: true},
		{name: "tokens apart from the file", secrets: memory{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := auth.Store{Dir: filepath.Join(t.TempDir(), "packrat"), Secrets: tc.secrets}
			if _, err := store.Current(); !errors.Is(err, auth.ErrNotSignedIn) {
				t.Fatalf("an empty store answers %v, want ErrNotSignedIn", err)
			}

			for _, org := range []string{"org_b", "org_a"} {
				if err := store.Save(auth.SignIn{Org: org, Email: "dana@example.com", Token: "packrat_api_" + org}); err != nil {
					t.Fatalf("saving %s: %v", org, err)
				}
			}
			current, err := store.Current()
			if err != nil || current.Org != "org_a" || current.Token != "packrat_api_org_a" {
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

			_, all, err := store.All()
			if err != nil || len(all) != 2 || all[0].Token != "" {
				t.Errorf("All is %+v (%v), want two sign-ins and no token", all, err)
			}

			if err := store.Remove("org_a"); err != nil {
				t.Fatalf("removing: %v", err)
			}
			current, err = store.Current()
			if err != nil || current.Org != "org_b" {
				t.Errorf("after removing the current one, current is %+v (%v), want org_b", current, err)
			}
			if err := store.Remove("org_b"); err != nil {
				t.Fatalf("removing: %v", err)
			}
			if _, err := store.Current(); !errors.Is(err, auth.ErrNotSignedIn) {
				t.Errorf("after removing all, current answers %v, want ErrNotSignedIn", err)
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
