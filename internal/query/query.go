// Package query asks query.packrat.sh what Packrat receives. It is the only code that sends a
// request, and it sends one thing of the caller's: the API key, as the Authorization header.
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
var ErrRefused = errors.New("the key was refused (401). It is wrong or revoked. Copy it again from the setup screen")

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

// Client reads the API with one key.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
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

func (c Client) get(ctx context.Context, path string, into any) (err error) {
	address := strings.TrimRight(c.BaseURL, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return fmt.Errorf("building the request to %s: %w", c.BaseURL, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.Key)
	request.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("asking %s: %w", c.BaseURL, err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing the answer of %s: %w", c.BaseURL, closeErr)
		}
	}()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody))
	if err != nil {
		return fmt.Errorf("reading the answer of %s: %w", c.BaseURL, err)
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return ErrRefused
	case http.StatusNotFound:
		// The API says in words which apps there are. A 404 with no sentence is an address it does not have.
		if said := message(body, ""); said != "" {
			return &NotFoundError{Message: said}
		}
		return fmt.Errorf("%s has no %s (404). The API is older than this command", c.BaseURL, strings.SplitN(path, "?", 2)[0])
	default:
		return fmt.Errorf("%s answered %d: %s", c.BaseURL, response.StatusCode, message(body, "no reason given"))
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("reading the answer of %s: %w", c.BaseURL, err)
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
