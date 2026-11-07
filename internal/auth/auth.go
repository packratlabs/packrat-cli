// Package auth keeps the sign-in that packrat login makes, in credentials.json under the user's config
// directory, with the token itself in the macOS Keychain where there is one. The key belongs to the
// person and opens each of their orgs, so there is one sign-in, not one for each org. It sends nothing.
// The caller must never print a token it returns.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotSignedIn is no sign-in kept.
var ErrNotSignedIn = errors.New("you're not signed in. Run packrat login")

// account names the token in Secrets.
const account = "login"

// SignIn is the person signed in. Token is empty in the file when Secrets keeps it.
type SignIn struct {
	Email string `json:"email"`
	Key   string `json:"key"`
	Token string `json:"token,omitempty"`
}

// Secrets keeps the token apart from the file.
type Secrets interface {
	Get(account string) (string, error)
	Set(account, token string) error
	Delete(account string) error
}

// Store is the file, and where its token goes. With no Secrets, the token is in the file.
type Store struct {
	Dir     string
	Secrets Secrets
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

// Current is the sign-in, with its token.
func (s Store) Current() (SignIn, error) {
	signIn, err := s.read()
	if err != nil {
		return SignIn{}, err
	}
	if s.Secrets == nil {
		return signIn, nil
	}
	token, err := s.Secrets.Get(account)
	if err != nil {
		return SignIn{}, fmt.Errorf("reading the token: %w", err)
	}
	signIn.Token = token
	return signIn, nil
}

// Save keeps a sign-in in place of the one before.
func (s Store) Save(signIn SignIn) error {
	if s.Secrets != nil {
		if err := s.Secrets.Set(account, signIn.Token); err != nil {
			return fmt.Errorf("keeping the token: %w", err)
		}
		signIn.Token = ""
	}
	return s.write(signIn)
}

// Remove forgets the sign-in.
func (s Store) Remove() error {
	if s.Secrets != nil {
		if err := s.Secrets.Delete(account); err != nil {
			return fmt.Errorf("deleting the token: %w", err)
		}
	}
	if err := os.Remove(s.path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", s.path(), err)
	}
	return nil
}

func (s Store) path() string { return filepath.Join(s.Dir, "credentials.json") }

func (s Store) read() (SignIn, error) {
	var signIn SignIn
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return signIn, ErrNotSignedIn
	}
	if err != nil {
		return signIn, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if err := json.Unmarshal(data, &signIn); err != nil {
		return signIn, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if signIn.Key == "" {
		return signIn, ErrNotSignedIn
	}
	return signIn, nil
}

func (s Store) write(signIn SignIn) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", s.Dir, err)
	}
	data, err := json.MarshalIndent(signIn, "", "  ")
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
