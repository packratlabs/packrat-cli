package query

import (
	"context"
	"net/http"
	"time"
)

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
		// SettingsURL is where an owner of the account changes what Packrat may do.
		SettingsURL string `json:"settings_url"`
	} `json:"github"`
	Connected  bool       `json:"connected"`
	Apps       []SetupApp `json:"apps"`
	Production *struct {
		App        string     `json:"app"`
		LastDataAt *time.Time `json:"last_data_at"`
		Version    string     `json:"version"`
	} `json:"production"`
	Pull     *SetupPull `json:"pull"`
	SetupURL string     `json:"setup_url"`
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
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	Declared   []string   `json:"declared"`
	None       string     `json:"none"`
	Version    string     `json:"version"`
	LiveAt     *time.Time `json:"live_at"`
	Checks     []Check    `json:"checks"`
	CommentURL string     `json:"comment_url"`
	// CommentError is what GitHub said when it refused Packrat's comment.
	CommentError string `json:"comment_error"`
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

// ConnectRepository connects the client's repository to the org's app, or to its next one when nothing reports yet.
func (c Client) ConnectRepository(ctx context.Context) (Setup, error) {
	var setup Setup
	body, err := c.send(ctx, http.MethodPost, "/setup/repository", "", "application/json", nil)
	if err != nil {
		return setup, err
	}
	return setup, decode(c.BaseURL, body, &setup)
}
