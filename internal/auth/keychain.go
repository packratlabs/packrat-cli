package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	service      = "packrat"
	notFound     = 44
	commandLimit = 10 * time.Second
)

// Keychain keeps tokens in the macOS login keychain through the security tool. A token goes to it on
// stdin, never as an argument, so it is not in the process list.
type Keychain struct{}

// Get reads the token of org.
func (Keychain) Get(org string) (string, error) {
	out, err := security(nil, "find-generic-password", "-s", service, "-a", org, "-w")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Set keeps token for org, in place of any token it had.
func (Keychain) Set(org, token string) error {
	command := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", service, quote(org), quote(token))
	if _, err := security([]byte(command), "-i"); err != nil {
		// Only the exit: what security -i prints about a failed line can repeat the line, token and all.
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("security add-generic-password: %w", exit)
		}
		return errors.New("security add-generic-password failed")
	}
	return nil
}

// Delete forgets the token of org. A token that is not there is not an error.
func (Keychain) Delete(org string) error {
	_, err := security(nil, "delete-generic-password", "-s", service, "-a", org)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == notFound {
		return nil
	}
	return err
}

func security(stdin []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/security", args...) // #nosec G204 -- fixed words, and an org id, which is never the token
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("security %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// quote makes a word for security -i, which splits its commands at spaces like a shell.
func quote(word string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(word) + `"`
}
