// Package auth keeps the sign-ins that packrat login makes: one for each org, in credentials.json
// under the user's config directory, with the token itself in the macOS Keychain where there is one.
// It sends nothing. The caller must never print a token it returns.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ErrNotSignedIn is no sign-in kept.
var ErrNotSignedIn = errors.New("not signed in. Run packrat login")

// SignIn is one org a person signed in to. Token is empty in the file when Secrets keeps it.
type SignIn struct {
	Org     string `json:"org"`
	OrgName string `json:"org_name"`
	Email   string `json:"email"`
	Key     string `json:"key"`
	Token   string `json:"token,omitempty"`
}

// Secrets keeps tokens apart from the file, by org.
type Secrets interface {
	Get(org string) (string, error)
	Set(org, token string) error
	Delete(org string) error
}

// Store is the file, and where its tokens go. With no Secrets, the tokens are in the file.
type Store struct {
	Dir     string
	Secrets Secrets
}

type file struct {
	Current string            `json:"current"`
	SignIns map[string]SignIn `json:"sign_ins"`
}

// Dir is where the file lives: $XDG_CONFIG_HOME/packrat, else ~/.config/packrat.
func Dir(getenv func(string) string) (string, error) {
	if base := getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "packrat"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "packrat"), nil
}

// Current is the sign-in that commands use: the last one made.
func (s Store) Current() (SignIn, error) {
	f, err := s.read()
	if err != nil {
		return SignIn{}, err
	}
	signIn, found := f.SignIns[f.Current]
	if !found {
		return SignIn{}, ErrNotSignedIn
	}
	return s.withToken(signIn)
}

// All is every sign-in, by org, with no tokens.
func (s Store) All() (current string, signIns []SignIn, err error) {
	f, err := s.read()
	if err != nil {
		return "", nil, err
	}
	for _, signIn := range f.SignIns {
		signIn.Token = ""
		signIns = append(signIns, signIn)
	}
	sort.Slice(signIns, func(i, j int) bool { return signIns[i].Org < signIns[j].Org })
	return f.Current, signIns, nil
}

// Save keeps a sign-in and makes it the current one. A sign-in to the same org replaces the old one.
func (s Store) Save(signIn SignIn) error {
	f, err := s.read()
	if err != nil && !errors.Is(err, ErrNotSignedIn) {
		return err
	}
	if s.Secrets != nil {
		if err := s.Secrets.Set(signIn.Org, signIn.Token); err != nil {
			return fmt.Errorf("keeping the token: %w", err)
		}
		signIn.Token = ""
	}
	f.SignIns[signIn.Org] = signIn
	f.Current = signIn.Org
	return s.write(f)
}

// Remove forgets the sign-in to org. The current one becomes another, when there is one.
func (s Store) Remove(org string) error {
	f, err := s.read()
	if err != nil {
		return err
	}
	delete(f.SignIns, org)
	if s.Secrets != nil {
		if err := s.Secrets.Delete(org); err != nil {
			return fmt.Errorf("deleting the token: %w", err)
		}
	}
	if f.Current == org {
		f.Current = ""
		for other := range f.SignIns {
			if f.Current == "" || other < f.Current {
				f.Current = other
			}
		}
	}
	return s.write(f)
}

func (s Store) withToken(signIn SignIn) (SignIn, error) {
	if s.Secrets == nil {
		return signIn, nil
	}
	token, err := s.Secrets.Get(signIn.Org)
	if err != nil {
		return SignIn{}, fmt.Errorf("reading the token for %s: %w", signIn.Org, err)
	}
	signIn.Token = token
	return signIn, nil
}

func (s Store) path() string { return filepath.Join(s.Dir, "credentials.json") }

func (s Store) read() (file, error) {
	f := file{SignIns: map[string]SignIn{}}
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return f, ErrNotSignedIn
	}
	if err != nil {
		return f, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if f.SignIns == nil {
		f.SignIns = map[string]SignIn{}
	}
	return f, nil
}

func (s Store) write(f file) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", s.Dir, err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	// Written beside it and renamed, so a crash leaves the old file or the new one, never half of one.
	partial := s.path() + ".partial"
	if err := os.WriteFile(partial, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", partial, err)
	}
	if err := os.Rename(partial, s.path()); err != nil {
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	return nil
}
