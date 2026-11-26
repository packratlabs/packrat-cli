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
	// Unread is whether it changed since the person whose key asks last opened it; absent for an org's key.
	Unread *bool `json:"unread,omitempty"`
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
	Status string
	Kind   string
	Sort   string
	Period string
	Limit  int
}

// Issues lists the app's issues.
func (c Client) Issues(ctx context.Context, where Where, filter IssueFilter) (Issues, error) {
	values := where.values()
	for name, value := range map[string]string{"status": filter.Status, "kind": filter.Kind, "sort": filter.Sort, "period": filter.Period} {
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

// IssueDetail is one issue as GET /issues/:id answers it: the issue, the releases it spans, the tags of its
// events with the commonest value of each, and its latest event. An AI-detected issue has no events of its own.
type IssueDetail struct {
	Issue
	FirstRelease string              `json:"first_release"`
	LastRelease  string              `json:"last_release"`
	Tags         map[string]TagValue `json:"tags"`
	Event        *IssueEvent         `json:"event"`
}

// TagValue is the commonest value of one tag over an issue's latest events, and how many carried it.
type TagValue struct {
	Top   string `json:"top"`
	Count int    `json:"count"`
}

// IssueEvent is one occurrence of an error, with its exception chain oldest cause first.
type IssueEvent struct {
	EventID     string           `json:"event_id"`
	OccurredAt  time.Time        `json:"occurred_at"`
	Environment string           `json:"environment"`
	Release     string           `json:"release"`
	Transaction string           `json:"transaction"`
	TraceID     string           `json:"trace_id"`
	User        json.RawMessage  `json:"user,omitempty"`
	Handled     *bool            `json:"handled"`
	Exceptions  []EventException `json:"exceptions"`
}

// EventException is one exception of an event's chain, its frames innermost last.
type EventException struct {
	Type   string       `json:"type"`
	Value  string       `json:"value"`
	Frames []EventFrame `json:"frames"`
}

// EventFrame is one line of a stack trace.
type EventFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno"`
	InApp    *bool  `json:"in_app"`
}

// Issue reads issue id with its latest event.
func (c Client) Issue(ctx context.Context, id string, where Where) (IssueDetail, error) {
	var issue IssueDetail
	if err := c.get(ctx, "/issues/"+url.PathEscape(id)+query(where.values()), &issue); err != nil {
		return IssueDetail{}, err
	}
	return issue, nil
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

// SeenIssue marks issue id seen by the person whose key asks.
func (c Client) SeenIssue(ctx context.Context, id string, where Where) (Issue, error) {
	body, err := c.act(ctx, http.MethodPut, "/issues/"+url.PathEscape(id)+"/seen"+query(where.values()), "")
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
