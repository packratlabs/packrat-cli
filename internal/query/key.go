package query

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// KeyName is the one variable whose value this program reads.
const KeyName = "PACKRAT_API_KEY"

// ErrNoKey is no key in the environment and none in the env file.
var ErrNoKey = errors.New("no " + KeyName + " in the environment or in .env")

// Key finds the API key: in the environment first, then in the .env file of dir. It reads the
// value of no other variable, and the caller must never print what it returns.
func Key(getenv func(string) string, dir string) (key string, err error) {
	if key := strings.TrimSpace(getenv(KeyName)); key != "" {
		return key, nil
	}
	file, err := os.Open(filepath.Join(dir, ".env")) // #nosec G304 -- the caller's own directory
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("opening .env: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing .env: %w", closeErr)
		}
	}()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		name, value, found := strings.Cut(strings.TrimPrefix(strings.TrimSpace(lines.Text()), "export "), "=")
		if !found || strings.TrimSpace(name) != KeyName {
			continue
		}
		if key := strings.Trim(strings.TrimSpace(value), `"'`); key != "" {
			return key, nil
		}
	}
	if err := lines.Err(); err != nil {
		return "", fmt.Errorf("reading .env: %w", err)
	}
	return "", ErrNoKey
}
