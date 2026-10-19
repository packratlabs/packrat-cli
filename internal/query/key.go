package query

import (
	"errors"
	"fmt"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

// KeyName is the variable that gives a key where nobody signs in, such as CI.
const KeyName = "PACKRAT_API_KEY"

// ErrNoKey is no key in the environment and no sign-in.
var ErrNoKey = errors.New("you're not signed in. Run packrat login, or set " + KeyName)

// SignIns is where packrat login keeps its sign-ins.
type SignIns interface {
	Current() (auth.SignIn, error)
}

// Key finds the API key: PACKRAT_API_KEY first, then the current sign-in. The caller must never print it.
func Key(getenv func(string) string, signIns SignIns) (string, error) {
	if key := strings.TrimSpace(getenv(KeyName)); key != "" {
		return key, nil
	}
	signIn, err := signIns.Current()
	if errors.Is(err, auth.ErrNotSignedIn) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("reading the sign-in: %w", err)
	}
	if signIn.Token == "" {
		return "", ErrNoKey
	}
	return signIn.Token, nil
}
