package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FailedCheck is a check of a pull request in the queue that failed and nobody excused.
type FailedCheck struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Claim    string `json:"claim"`
	Expected string `json:"expected"`
	Instead  string `json:"instead"`
	Outcome  string `json:"outcome"`
	Source   string `json:"source"`
	SQL      string `json:"sql,omitempty"`
	PassWhen string `json:"pass_when,omitempty"`
	FailWhen string `json:"fail_when,omitempty"`
}

// Item is a pull request that failed in production and waits for a loop.
type Item struct {
	Kind         string        `json:"kind"`
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	App          string        `json:"app"`
	Repository   string        `json:"repository"`
	Verdict      string        `json:"verdict"`
	DeployedAt   time.Time     `json:"deployed_at"`
	Version      string        `json:"version"`
	JudgedAt     *time.Time    `json:"judged_at"`
	Evidence     string        `json:"evidence"`
	TakenAt      *time.Time    `json:"taken_at"`
	Issues       []IssueLink   `json:"issues"`
	FailedChecks []FailedCheck `json:"failed_checks"`
}

// Queue is the answer of GET /queue: the pull requests that wait, and the ones a loop holds.
type Queue struct {
	App     string `json:"app"`
	Waiting []Item `json:"waiting"`
	Taken   []Item `json:"taken"`
}

// Next is the answer of POST /queue/next: the pull request taken, or none, and how many still wait.
type Next struct {
	App     string `json:"app"`
	Item    *Item  `json:"item"`
	Waiting int    `json:"waiting"`
}

// Ending is how a loop ended a pull request. NewCheck is the check that replaces the failed one.
type Ending struct {
	Ending   string          `json:"ending"`
	Note     string          `json:"note"`
	Check    string          `json:"check,omitempty"`
	Fix      string          `json:"fix,omitempty"`
	NewCheck json.RawMessage `json:"new_check,omitempty"`
}

// Ended is the answer of PUT /queue/:number: the ending kept, and what it changed.
type Ended struct {
	App     string `json:"app"`
	Pull    int    `json:"pull"`
	Ending  string `json:"ending"`
	Note    string `json:"note"`
	Fix     string `json:"fix,omitempty"`
	State   string `json:"state"`
	Excused string `json:"excused,omitempty"`
	Added   string `json:"added,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// Queue lists the failed pull requests of the app that wait for a loop.
func (c Client) Queue(ctx context.Context, where Where) (Queue, error) {
	var queue Queue
	if err := c.get(ctx, "/queue"+query(where.values()), &queue); err != nil {
		return Queue{}, err
	}
	if queue.Waiting == nil {
		queue.Waiting = []Item{}
	}
	if queue.Taken == nil {
		queue.Taken = []Item{}
	}
	return queue, nil
}

// Next takes the next failed pull request, which no other loop is then handed.
func (c Client) Next(ctx context.Context, where Where) (Next, error) {
	body, err := c.send(ctx, "POST", "/queue/next"+query(where.values()), "", "application/json", nil)
	if err != nil {
		return Next{}, err
	}
	var next Next
	return next, decode(c.BaseURL, body, &next)
}

// End reports how a loop ended a pull request, which makes the change the ending names.
func (c Client) End(ctx context.Context, number int, where Where, ending Ending) (Ended, error) {
	payload, err := json.Marshal(ending)
	if err != nil {
		return Ended{}, fmt.Errorf("writing the ending: %w", err)
	}
	body, err := c.send(ctx, "PUT", "/queue/"+strconv.Itoa(number)+query(where.values()), "text/plain", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return Ended{}, err
	}
	var ended Ended
	return ended, decode(c.BaseURL, body, &ended)
}
