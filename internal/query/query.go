// Package query asks Packrat: query.packrat.sh for data, and packrat.sh for its pages and to sign
// in. It also asks GitHub for the CLI's releases, with no key. It is the only code that sends a
// request. Of the caller's, it sends the API key, as the Authorization header, and only to a Packrat host.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultURL is where the API lives. PACKRAT_QUERY_URL replaces it.
const DefaultURL = "https://query.packrat.sh"

const maxBody = 4 << 20

// ErrRefused is a key that the API does not accept.
var ErrRefused = errors.New("your key was rejected (401). It may be invalid or revoked. Run packrat login again")

// UserAgent names this program to the API, followed by /version.
const UserAgent = "packrat-cli"

// Heard is what the API's answers said about the CLI's releases. The newest answer wins.
type Heard struct {
	Latest  string
	Minimum string
}

// TooOldError is the API refusing this version of the CLI (426). Message says how to update.
type TooOldError struct{ Message string }

func (e *TooOldError) Error() string { return e.Message }

// AppRequiredError is a request that named no app when the key opens more than one. Apps are the
// services to choose from.
type AppRequiredError struct{ Apps []string }

func (e *AppRequiredError) Error() string {
	apps := e.Apps
	list := strings.Join(apps, ", ")
	if len(apps) > 1 {
		list = strings.Join(apps[:len(apps)-1], ", ") + " and " + apps[len(apps)-1]
	}
	return "No app specified. This key has access to " + list + "."
}

// NotFoundError is an app that is not there. Message is the API's sentence, which names the apps that are.
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }

// App is one service and environment that has sent data.
type App struct {
	App         string     `json:"app"`
	Service     string     `json:"service"`
	Env         string     `json:"env"`
	FirstSeenAt time.Time  `json:"first_seen_at"`
	LastDataAt  *time.Time `json:"last_data_at"`
}

// Apps is the answer of GET /apps. Now is the API's clock, so that an age needs no clock of ours.
type Apps struct {
	Now  time.Time `json:"now"`
	Apps []App     `json:"apps"`
}

// Source is one sender of a signal. Kept is false when the app keeps that signal from another source.
type Source struct {
	Source     string     `json:"source"`
	Name       string     `json:"name"`
	Kept       bool       `json:"kept"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// Signal is one of exceptions, traces, logs, metrics and replays, with every source that sends it.
// Quiet means the kept source stopped and another source sends the signal now.
type Signal struct {
	Signal  string   `json:"signal"`
	Quiet   bool     `json:"quiet"`
	Sources []Source `json:"sources"`
}

// Status is the answer of GET /status for one app.
type Status struct {
	App
	Now      time.Time `json:"now"`
	Signals  []Signal  `json:"signals"`
	Warnings []string  `json:"warnings"`
}

// Client reads the API with one key. Version goes in the User-Agent, and when Heard is set, the
// release headers of each answer are kept in it.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
	Version string
	Heard   *Heard
}

// Apps lists the apps the key opens.
func (c Client) Apps(ctx context.Context) (Apps, error) {
	var apps Apps
	if err := c.get(ctx, "/apps", &apps); err != nil {
		return Apps{}, err
	}
	if apps.Apps == nil {
		apps.Apps = []App{}
	}
	return apps, nil
}

// Status says what Packrat receives for app, which is service/env.
func (c Client) Status(ctx context.Context, app string) (Status, error) {
	var status Status
	if err := c.get(ctx, "/status?app="+url.QueryEscape(app), &status); err != nil {
		return Status{}, err
	}
	if status.Warnings == nil {
		status.Warnings = []string{}
	}
	for i := range status.Signals {
		if status.Signals[i].Sources == nil {
			status.Signals[i].Sources = []Source{}
		}
	}
	return status, nil
}

func (c Client) get(ctx context.Context, path string, into any) error {
	body, err := c.send(ctx, http.MethodGet, path, "", "application/json", nil)
	if err != nil {
		return err
	}
	return decode(c.BaseURL, body, into)
}

// send asks the API. On an error the body is the API's answer, when there was one.
func (c Client) send(ctx context.Context, method, path, contentType, accept string, payload io.Reader) ([]byte, error) {
	address := strings.TrimRight(c.BaseURL, "/") + path
	if payload == nil {
		payload = http.NoBody
	}
	request, err := http.NewRequestWithContext(ctx, method, address, payload)
	if err != nil {
		return nil, fmt.Errorf("building the request to %s: %w", c.BaseURL, err)
	}
	body, _, err := c.do(request, contentType, accept, path)
	return body, err
}

func (c Client) do(request *http.Request, contentType, accept, path string) (body []byte, kind string, err error) {
	where := request.URL.Scheme + "://" + request.URL.Host
	request.Header.Set("Authorization", "Bearer "+c.Key)
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", UserAgent+"/"+c.version())
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.HTTP.Do(request) // #nosec G704 -- a base URL, or an address Page has checked is Packrat's
	if err != nil {
		return nil, "", fmt.Errorf("asking %s: %w", where, err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing the answer of %s: %w", where, closeErr)
		}
	}()
	body, err = io.ReadAll(io.LimitReader(response.Body, maxBody))
	if err != nil {
		return nil, "", fmt.Errorf("reading the answer of %s: %w", where, err)
	}
	c.hear(response.Header)
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return body, response.Header.Get("Content-Type"), nil
	case response.StatusCode == http.StatusUnauthorized:
		return body, "", ErrRefused
	case response.StatusCode == http.StatusUpgradeRequired:
		return body, "", &TooOldError{Message: message(body, "This packrat is too old for "+where+". Run packrat update.")}
	case response.StatusCode == http.StatusNotFound:
		// The API says in words which apps there are. A 404 with no sentence is an address it does not have.
		if said := message(body, ""); said != "" {
			return body, "", &NotFoundError{Message: said}
		}
		return body, "", fmt.Errorf("%s has no %s (404). The API is older than this command", where, strings.SplitN(path, "?", 2)[0])
	default:
		var choose struct {
			Apps []string `json:"apps"`
		}
		if response.StatusCode == http.StatusBadRequest && json.Unmarshal(body, &choose) == nil && len(choose.Apps) > 0 {
			return body, "", &AppRequiredError{Apps: choose.Apps}
		}
		return body, "", fmt.Errorf("%s answered %d: %s", where, response.StatusCode, message(body, "no reason given"))
	}
}

func (c Client) version() string {
	if c.Version == "" {
		return "dev"
	}
	return c.Version
}

func (c Client) hear(header http.Header) {
	if c.Heard == nil {
		return
	}
	if latest := header.Get("Packrat-CLI-Latest"); latest != "" {
		c.Heard.Latest = latest
	}
	if minimum := header.Get("Packrat-CLI-Minimum"); minimum != "" {
		c.Heard.Minimum = minimum
	}
}

func decode(base string, body []byte, into any) error {
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("reading the answer of %s: %w", base, err)
	}
	return nil
}

// message is the sentence of an error answer, or fallback when the body has none.
func message(body []byte, fallback string) string {
	var answer struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Message == "" {
		return fallback
	}
	return answer.Message
}
