package query

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	RunsAt   *time.Time `json:"runs_at,omitempty"`
	Pull     int        `json:"pull,omitempty"`
	Asked    bool       `json:"asked"`
	OpenedAt time.Time  `json:"opened_at"`
	ClosedAt *time.Time `json:"closed_at"`
	Outcome  string     `json:"outcome"`
	Title    string     `json:"title"`
	Source   string     `json:"source"`
	Result   string     `json:"result"`
	Accepted string     `json:"accepted"`
	Runs     []Run      `json:"runs"`
	// A rule check runs every five minutes until its window ends (DueAt); Seen is from its latest run.
	Kind         string     `json:"kind,omitempty"`
	SQL          string     `json:"sql,omitempty"`
	PassWhen     string     `json:"pass_when,omitempty"`
	FailWhen     string     `json:"fail_when,omitempty"`
	MinSeen      int64      `json:"min_seen,omitempty"`
	AtEnd        string     `json:"at_end,omitempty"`
	WindowEndsAt *time.Time `json:"window_ends_at,omitempty"`
	Seen         *int64     `json:"seen,omitempty"`
	Review       string     `json:"review,omitempty"`
}

// Run is one time the analyst verified a check.
type Run struct {
	At       time.Time `json:"at"`
	Result   string    `json:"result"`
	Evidence string    `json:"evidence"`
}

// Pull is the answer of GET /pulls/:number.
type Pull struct {
	App   string `json:"app"`
	Org   string `json:"org"`
	Pull  int    `json:"pull"`
	State string `json:"state"`
	// ArchiveReason is why a member archived its verification, if they said.
	ArchiveReason string     `json:"archive_reason,omitempty"`
	Analysed      bool       `json:"analysed"`
	Repository    string     `json:"repository"`
	Deploys       []Deploy   `json:"deploys"`
	Checks        []Check    `json:"checks"`
	Declared      []Declared `json:"declared"`
	// Dropped are checks the coding agent could not make run, sent so they are seen; they never run.
	Dropped []Declared `json:"dropped"`
}

// Declared is a check a pull request declared, waiting for the deploy that ships it. Window is how
// long after that deploy it stays open, such as "1h". Dropped is why the coding agent gave up on it.
type Declared struct {
	Title   string `json:"title"`
	Claim   string `json:"claim"`
	Window  string `json:"window"`
	Dropped string `json:"dropped,omitempty"`
	Check   string `json:"check,omitempty"`
}

// Declaration is the answer of PUT /pulls/:number/checks. Pull is 0 on a dry run, and None is why a
// pull request declared no checks.
type Declaration struct {
	App    string     `json:"app"`
	Pull   int        `json:"pull"`
	DryRun bool       `json:"dry_run"`
	None   string     `json:"none,omitempty"`
	Checks []Declared `json:"checks"`
	// Warnings name checks that already pass before the deploy, so passing would prove nothing.
	Warnings []string `json:"warnings,omitempty"`
}

// Declare sends the checks a pull request ships with, which replace the ones it declared before. A dry
// run with number 0 validates a draft before the pull request has a number, and keeps nothing.
func (c Client) Declare(ctx context.Context, number int, where Where, draft []byte, dryRun bool) (Declaration, error) {
	path := "/pulls/checks"
	if number > 0 {
		path = "/pulls/" + strconv.Itoa(number) + "/checks"
	}
	values := where.values()
	if dryRun {
		values.Set("dry_run", "1")
	}
	var declared Declaration
	body, err := c.send(ctx, "PUT", path+query(values), "text/plain", "application/json", strings.NewReader(string(draft)))
	if err != nil {
		return Declaration{}, err
	}
	if err := decode(c.BaseURL, body, &declared); err != nil {
		return Declaration{}, err
	}
	if declared.Checks == nil {
		declared.Checks = []Declared{}
	}
	return declared, nil
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
	if pull.Declared == nil {
		pull.Declared = []Declared{}
	}
	return pull, nil
}

// ArchivePull archives a pending verification and cancels its open checks; with archived false it moves the
// verification back and opens them again.
func (c Client) ArchivePull(ctx context.Context, number int, where Where, archived bool, reason string) (Pull, error) {
	method := http.MethodPatch
	if !archived {
		method = http.MethodDelete
	}
	body, err := c.act(ctx, method, "/pulls/"+strconv.Itoa(number)+"/archive"+query(where.values()), reason)
	if err != nil {
		return Pull{}, err
	}
	var pull Pull
	return pull, decode(c.BaseURL, body, &pull)
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
	JSON   string
	Pull   int
	Report string
}

// Check asks the analyst to verify something at a later time.
func (c Client) Check(ctx context.Context, where Where, ask Ask) (Check, error) {
	values := where.values()
	if ask.Pull > 0 {
		values.Set("pull", strconv.Itoa(ask.Pull))
	}
	if ask.Report != "" {
		values.Set("report", ask.Report)
	}
	var check Check
	body, err := c.send(ctx, "POST", "/checks"+query(values), "text/plain", "application/json", strings.NewReader(ask.JSON))
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
