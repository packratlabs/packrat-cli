package query

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Where names the app a request is about: by its name, or by the GitHub repository it is connected
// to. Both empty lets the API pick the org's only app.
type Where struct {
	App  string
	Repo string
}

func (w Where) values() url.Values {
	values := url.Values{}
	switch {
	case w.App != "":
		values.Set("app", w.App)
	case w.Repo != "":
		values.Set("repo", w.Repo)
	}
	return values
}

// Deploy is one version the app started running, and the analyst's verdict on it.
type Deploy struct {
	ID         string     `json:"id"`
	Version    string     `json:"version"`
	DeployedAt time.Time  `json:"deployed_at"`
	Pull       int        `json:"pull"`
	Title      string     `json:"title"`
	Verdict    string     `json:"verdict"`
	JudgedAt   *time.Time `json:"judged_at"`
	Intent     string     `json:"intent"`
	Evidence   string     `json:"evidence"`
	NextStep   string     `json:"next_step"`
	FollowUp   string     `json:"follow_up"`
	Report     string     `json:"report"`
	Check      string     `json:"check"`
}

// Watch is a thing the analyst is due to verify.
type Watch struct {
	ID       string     `json:"id"`
	What     string     `json:"what"`
	DueAt    time.Time  `json:"due_at"`
	Pull     int        `json:"pull"`
	Asked    bool       `json:"asked"`
	OpenedAt time.Time  `json:"opened_at"`
	ClosedAt *time.Time `json:"closed_at"`
	Outcome  string     `json:"outcome"`
}

// Pull is the answer of GET /pulls/:number.
type Pull struct {
	App        string   `json:"app"`
	Pull       int      `json:"pull"`
	Analysed   bool     `json:"analysed"`
	Repository string   `json:"repository"`
	Deploys    []Deploy `json:"deploys"`
	Watches    []Watch  `json:"watches"`
}

// Watches is the answer of GET /watches.
type Watches struct {
	App     string  `json:"app"`
	Watches []Watch `json:"watches"`
}

// Pull is how pull request number went in production.
func (c Client) Pull(ctx context.Context, number int, where Where) (Pull, error) {
	var pull Pull
	if err := c.get(ctx, "/pulls/"+strconv.Itoa(number)+query(where.values()), &pull); err != nil {
		return Pull{}, err
	}
	if pull.Deploys == nil {
		pull.Deploys = []Deploy{}
	}
	if pull.Watches == nil {
		pull.Watches = []Watch{}
	}
	return pull, nil
}

// Watches lists the app's open watches, and the asked ones closed this week. A pull above 0 keeps the
// ones asked for that pull request.
func (c Client) Watches(ctx context.Context, where Where, pull int) (Watches, error) {
	values := where.values()
	if pull > 0 {
		values.Set("pull", strconv.Itoa(pull))
	}
	var watches Watches
	if err := c.get(ctx, "/watches"+query(values), &watches); err != nil {
		return Watches{}, err
	}
	if watches.Watches == nil {
		watches.Watches = []Watch{}
	}
	return watches, nil
}

// Watch asks the analyst to verify text at due, for pull request pull when it is above 0.
func (c Client) Watch(ctx context.Context, where Where, pull int, due time.Time, text string) (Watch, error) {
	values := where.values()
	values.Set("due", due.UTC().Format("2006-01-02T15:04Z"))
	if pull > 0 {
		values.Set("pull", strconv.Itoa(pull))
	}
	var watch Watch
	body, err := c.send(ctx, "POST", "/watches"+query(values), "text/plain", "application/json", strings.NewReader(text))
	if err != nil {
		return Watch{}, err
	}
	return watch, decode(c.BaseURL, body, &watch)
}

// CloseWatch closes a watch before it is verified.
func (c Client) CloseWatch(ctx context.Context, id string) (Watch, error) {
	var watch Watch
	body, err := c.send(ctx, "DELETE", "/watches/"+url.PathEscape(id), "", "application/json", nil)
	if err != nil {
		return Watch{}, err
	}
	return watch, decode(c.BaseURL, body, &watch)
}

// SQL runs a script against app and copies the NDJSON answer to out, and answers with its last line.
func (c Client) SQL(ctx context.Context, app, sql string, out io.Writer) (string, error) {
	values := url.Values{}
	if app != "" {
		values.Set("app", app)
	}
	body, err := c.send(ctx, "POST", "/sql"+query(values), "application/sql", "application/x-ndjson", strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	if _, err := out.Write(body); err != nil {
		return "", fmt.Errorf("writing the rows: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	return lines[len(lines)-1], nil
}

func query(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}
