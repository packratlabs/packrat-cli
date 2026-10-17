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
	Look       string     `json:"look"`
}

// Check is a thing the analyst is due to verify.
type Check struct {
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
	Checks     []Check  `json:"checks"`
}

// Checks is the answer of GET /checks.
type Checks struct {
	App    string  `json:"app"`
	Checks []Check `json:"checks"`
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
	if pull.Checks == nil {
		pull.Checks = []Check{}
	}
	return pull, nil
}

// Checks lists the app's open checks, and the asked ones closed this week. A pull above 0 keeps the
// ones asked for that pull request.
func (c Client) Checks(ctx context.Context, where Where, pull int) (Checks, error) {
	values := where.values()
	if pull > 0 {
		values.Set("pull", strconv.Itoa(pull))
	}
	var checks Checks
	if err := c.get(ctx, "/checks"+query(values), &checks); err != nil {
		return Checks{}, err
	}
	if checks.Checks == nil {
		checks.Checks = []Check{}
	}
	return checks, nil
}

// Ask is a check to ask for: what must be true and the SQL that shows it, from when, and the pull
// request or the analyst's mail it belongs to, if any.
type Ask struct {
	Text   string
	Due    time.Time
	Pull   int
	Report string
}

// Check asks the analyst to verify something at a later time.
func (c Client) Check(ctx context.Context, where Where, ask Ask) (Check, error) {
	values := where.values()
	values.Set("due", ask.Due.UTC().Format("2006-01-02T15:04Z"))
	if ask.Pull > 0 {
		values.Set("pull", strconv.Itoa(ask.Pull))
	}
	if ask.Report != "" {
		values.Set("report", ask.Report)
	}
	var check Check
	body, err := c.send(ctx, "POST", "/checks"+query(values), "text/plain", "application/json", strings.NewReader(ask.Text))
	if err != nil {
		return Check{}, err
	}
	return check, decode(c.BaseURL, body, &check)
}

// ReadCheck reads one check, open or closed.
func (c Client) ReadCheck(ctx context.Context, id string) (Check, error) {
	var check Check
	return check, c.get(ctx, "/checks/"+url.PathEscape(id), &check)
}

// CloseCheck closes a check before it is verified.
func (c Client) CloseCheck(ctx context.Context, id string) (Check, error) {
	var check Check
	body, err := c.send(ctx, "DELETE", "/checks/"+url.PathEscape(id), "", "application/json", nil)
	if err != nil {
		return Check{}, err
	}
	return check, decode(c.BaseURL, body, &check)
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
