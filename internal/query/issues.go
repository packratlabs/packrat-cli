package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Issue is one of the issues an app's errors are grouped into, or one the analyst raised (Kind "detected").
type Issue struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Culprit       string    `json:"culprit"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Substatus     string    `json:"substatus"`
	ResolvedBy    string    `json:"resolved_by"`
	ArchivedUntil string    `json:"archived_until"`
	Severity      string    `json:"severity"`
	Summary       string    `json:"summary"`
	Reason        string    `json:"reason,omitempty"`
	Level         string    `json:"level"`
	Platform      string    `json:"platform"`
	Events        int       `json:"events"`
	Users         int       `json:"users"`
	TimesSeen     int       `json:"times_seen"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
	PartOf        string    `json:"part_of"`
	Environment   string    `json:"environment,omitempty"`
	State         string    `json:"state,omitempty"`
	Normal        string    `json:"normal,omitempty"`
	Errors        []string  `json:"errors,omitempty"`
	Pull          int       `json:"pull,omitempty"`
	Reasons       []string  `json:"reasons,omitempty"`
}

// Issues is an app's issues, counted over Period.
type Issues struct {
	App    string  `json:"app"`
	Org    string  `json:"org"`
	Period string  `json:"period"`
	Issues []Issue `json:"issues"`
}

// IssueFilter narrows the list. An empty field is the API's default.
type IssueFilter struct {
	Status      string
	Kind        string
	Environment string
	Sort        string
	Period      string
	Limit       int
}

// Issues lists the app's issues.
func (c Client) Issues(ctx context.Context, where Where, filter IssueFilter) (Issues, error) {
	values := where.values()
	for name, value := range map[string]string{"status": filter.Status, "kind": filter.Kind, "environment": filter.Environment,
		"sort": filter.Sort, "period": filter.Period} {
		if value != "" {
			values.Set(name, value)
		}
	}
	if filter.Limit > 0 {
		values.Set("limit", strconv.Itoa(filter.Limit))
	}
	var issues Issues
	if err := c.get(ctx, "/issues"+query(values), &issues); err != nil {
		return Issues{}, err
	}
	if issues.Issues == nil {
		issues.Issues = []Issue{}
	}
	return issues, nil
}

// IssueAction is what a member does to an issue: resolve, archive or reopen it.
type IssueAction struct {
	Do     string
	Until  string
	Reason string
}

// ActOnIssue resolves, archives or reopens issue id, and answers with the issue as it is now.
func (c Client) ActOnIssue(ctx context.Context, id string, where Where, action IssueAction) (Issue, error) {
	values := where.values()
	method, path := http.MethodPatch, "/issues/"+url.PathEscape(id)
	switch action.Do {
	case "resolve":
		path += "/resolve"
	case "archive":
		path += "/archive"
		if action.Until != "" {
			values.Set("until", action.Until)
		}
	case "reopen":
		method, path = http.MethodDelete, path+"/resolve"
	default:
		return Issue{}, fmt.Errorf("%q is not something to do to an issue", action.Do)
	}
	body, err := c.act(ctx, method, path+query(values), action.Reason)
	if err != nil {
		return Issue{}, err
	}
	var issue Issue
	return issue, decode(c.BaseURL, body, &issue)
}

// act sends a change, with the reason a member gave for it in a JSON body.
func (c Client) act(ctx context.Context, method, path, reason string) ([]byte, error) {
	if reason == "" {
		return c.send(ctx, method, path, "", "application/json", nil)
	}
	payload, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return nil, fmt.Errorf("writing the reason: %w", err)
	}
	return c.send(ctx, method, path, "application/json", "application/json", strings.NewReader(string(payload)))
}
