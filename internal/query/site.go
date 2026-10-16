package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DefaultSiteURL is where the pages and the sign-in live. PACKRAT_URL replaces it.
const DefaultSiteURL = "https://packrat.sh"

// The answers of a poll before there is a token, by the names RFC 8628 gives them.
var (
	ErrPending  = errors.New("authorization_pending")
	ErrSlowDown = errors.New("slow_down")
	ErrDenied   = errors.New("the sign-in was denied in the browser")
	ErrExpired  = errors.New("the code expired or was used. Run packrat login again")
)

// ErrNotPackrat is an address that is not Packrat's, which is never sent the key.
var ErrNotPackrat = errors.New("not a Packrat address, so it is not sent the key")

// Started is the answer of POST /cli/sign_ins.
type Started struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Complete        string `json:"verification_uri_complete"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Token is the answer of POST /cli/token once the person has approved.
type Token struct {
	AccessToken string `json:"access_token"`
	Key         string `json:"key"`
	Org         string `json:"org"`
	OrgName     string `json:"org_name"`
	Email       string `json:"email"`
}

// Who is the answer of GET /cli/whoami: the key that asked.
type Who struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Org     string `json:"org"`
	OrgName string `json:"org_name"`
	Email   string `json:"email"`
}

// StartSignIn asks for a code for this machine, named name.
func (c Client) StartSignIn(ctx context.Context, name string) (Started, error) {
	var started Started
	body, err := c.send(ctx, http.MethodPost, "/cli/sign_ins", "application/x-www-form-urlencoded", "application/json",
		strings.NewReader(url.Values{"name": {name}}.Encode()))
	if err != nil {
		return Started{}, err
	}
	return started, decode(c.BaseURL, body, &started)
}

// PollToken asks whether the person has approved. Until then it answers ErrPending or ErrSlowDown.
func (c Client) PollToken(ctx context.Context, deviceCode string) (Token, error) {
	var token Token
	body, err := c.send(ctx, http.MethodPost, "/cli/token", "application/x-www-form-urlencoded", "application/json",
		strings.NewReader(url.Values{"device_code": {deviceCode}}.Encode()))
	if err != nil {
		var said struct {
			Error string `json:"error"`
		}
		if decode(c.BaseURL, body, &said) == nil {
			switch said.Error {
			case "authorization_pending":
				return Token{}, ErrPending
			case "slow_down":
				return Token{}, ErrSlowDown
			case "access_denied":
				return Token{}, ErrDenied
			case "expired_token", "invalid_grant":
				return Token{}, ErrExpired
			}
		}
		return Token{}, err
	}
	return token, decode(c.BaseURL, body, &token)
}

// WhoAmI names the key.
func (c Client) WhoAmI(ctx context.Context) (Who, error) {
	var who Who
	return who, c.get(ctx, "/cli/whoami", &who)
}

// Revoke revokes the key, which must be one that packrat login made.
func (c Client) Revoke(ctx context.Context) error {
	_, err := c.send(ctx, http.MethodDelete, "/cli/token", "", "application/json", nil)
	return err
}

// Page reads a Packrat address with the key, markdown where the page has it. The key goes only to
// packrat.sh, its subdomains, and the hosts of the base URLs in hosts.
func (c Client) Page(ctx context.Context, address string, hosts ...string) (body []byte, kind string, err error) {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, "", fmt.Errorf("%s: %w", address, ErrNotPackrat)
	}
	if !packratHost(parsed, hosts) {
		return nil, "", fmt.Errorf("%s: %w", parsed.Host, ErrNotPackrat)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("building the request to %s: %w", parsed.Host, err)
	}
	return c.do(request, "", "text/markdown, text/html;q=0.9, application/json;q=0.8", parsed.Path)
}

func packratHost(address *url.URL, bases []string) bool {
	host := address.Hostname()
	if address.Scheme == "https" && (host == "packrat.sh" || strings.HasSuffix(host, ".packrat.sh")) {
		return true
	}
	for _, base := range bases {
		if allowed, err := url.Parse(base); err == nil && allowed.Host == address.Host && allowed.Scheme == address.Scheme {
			return true
		}
	}
	return false
}
