package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// SetupRunEnv names the run of packrat setup a process is part of. Setup sets it for the Claude Code it starts, so what
// that session asks Packrat reads as part of the same run.
const SetupRunEnv = "PACKRAT_SETUP_RUN"

var setupRun = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

// SetupRun is the run PACKRAT_SETUP_RUN names, or "" when it names none.
func SetupRun(getenv func(string) string) string {
	if run := getenv(SetupRunEnv); setupRun.MatchString(run) {
		return run
	}
	return ""
}

// SetupReport is what a run of packrat setup printed: its rows so far and, once it ends, its exit code and last error.
type SetupReport struct {
	Phase string      `json:"phase"`
	Steps []SetupStep `json:"steps"`
	Exit  *int        `json:"exit,omitempty"`
	Error string      `json:"error,omitempty"`
}

// SetupStep is one row of packrat setup.
type SetupStep struct {
	Step string `json:"step"`
	Mark string `json:"mark"`
	Text string `json:"text,omitempty"`
}

// Setup is the answer of GET /setup/state on the site: how far setup has come for one repository.
type Setup struct {
	Repository string `json:"repository"`
	OrgOf      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"org"`
	GitHub struct {
		Configured bool   `json:"configured"`
		Readable   bool   `json:"readable"`
		Account    string `json:"account"`
		// Accounts is every GitHub account the org has approved Packrat for, whether or not it holds the repository.
		Accounts   []string `json:"accounts"`
		InstallURL string   `json:"install_url"`
	} `json:"github"`
	Connected bool `json:"connected"`
	// Unconnected are the org's services that read no repository, while this repository is connected to none.
	Unconnected []SetupService `json:"unconnected"`
	Apps        []SetupApp     `json:"apps"`
	Production  *struct {
		App        string     `json:"app"`
		LastDataAt *time.Time `json:"last_data_at"`
		Version    string     `json:"version"`
	} `json:"production"`
	Pull     *SetupPull `json:"pull"`
	SetupURL string     `json:"setup_url"`
	Now      time.Time  `json:"now"`
}

// SetupService is one of the org's services that reads no repository: an app that reported before setup ran.
type SetupService struct {
	Service    string     `json:"service"`
	Envs       []string   `json:"envs"`
	LastDataAt *time.Time `json:"last_data_at"`
}

// SetupApp is one app connected to the repository.
type SetupApp struct {
	App        string     `json:"app"`
	Env        string     `json:"env"`
	Analysed   bool       `json:"analysed"`
	LastDataAt *time.Time `json:"last_data_at"`
}

// SetupPull is the newest pull request of the repository that declared checks.
type SetupPull struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	State    string     `json:"state"`
	Declared []string   `json:"declared"`
	None     string     `json:"none"`
	Version  string     `json:"version"`
	LiveAt   *time.Time `json:"live_at"`
	Checks   []Check    `json:"checks"`
}

// In is the client asking for one org and one repository.
func (c Client) In(org, repo string) Client {
	c.Org, c.Repo = org, repo
	return c
}

// SetupState reads how far setup has come for the client's repository.
func (c Client) SetupState(ctx context.Context) (Setup, error) {
	var setup Setup
	return setup, c.get(ctx, "/setup/state", &setup)
}

// ConnectRepository connects the client's repository to the service named, an app that reported before setup ran, or
// else to the org's next app.
func (c Client) ConnectRepository(ctx context.Context, service string) (Setup, error) {
	var setup Setup
	path := "/setup/repository"
	if service != "" {
		path += "?" + url.Values{"service": {service}}.Encode()
	}
	body, err := c.send(ctx, http.MethodPost, path, "", "application/json", nil)
	if err != nil {
		return setup, err
	}
	return setup, decode(c.BaseURL, body, &setup)
}

// ReportSetup sends what a run of packrat setup printed to the site, which keeps it on the request's span.
func (c Client) ReportSetup(ctx context.Context, report SetupReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encoding the setup report: %w", err)
	}
	if _, err := c.send(ctx, http.MethodPost, "/setup/events", "application/json", "application/json", bytes.NewReader(body)); err != nil {
		return fmt.Errorf("reporting setup: %w", err)
	}
	return nil
}

// Feedback is a report to the Packrat team of what Packrat got in the way of: packrat feedback.
type Feedback struct {
	Task        string `json:"task"`
	Kind        string `json:"kind"`
	Impact      string `json:"impact"`
	About       string `json:"about,omitempty"`
	Description string `json:"description"`
}

// SendFeedback posts a report to the site, which mails it to the Packrat team, and gives back what the site said.
func (c Client) SendFeedback(ctx context.Context, feedback Feedback) (string, error) {
	body, err := json.Marshal(feedback)
	if err != nil {
		return "", fmt.Errorf("encoding the feedback: %w", err)
	}
	answer, err := c.send(ctx, http.MethodPost, "/feedback", "application/json", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sending feedback: %w", err)
	}
	var sent struct {
		Message string `json:"message"`
	}
	return sent.Message, decode(c.BaseURL, answer, &sent)
}
