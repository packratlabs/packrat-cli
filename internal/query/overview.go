package query

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Overview is the answer of GET /overview: what the analyst said about an app in the last day (Feed), and the
// latest daily summary with its items alone.
type Overview struct {
	App    string    `json:"app"`
	Org    string    `json:"org"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Feed   []Item    `json:"feed"`
	Report *Report   `json:"report"`
	Items  []Item    `json:"items"`
	// Raw is the answer as the API sent it, which --json writes so a field the API adds reaches the pane
	// without a release of the CLI.
	Raw json.RawMessage `json:"-"`
}

// Report names the summary the items come from. Read the whole of it with packrat open.
type Report struct {
	ID      string    `json:"id"`
	Subject string    `json:"subject"`
	At      time.Time `json:"at"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

// Item is one thing the analyst said: a summary's act, watch, shipped or better item, with its four sentences, or
// an urgent mail or update, with its markdown in Body.
type Item struct {
	Kind     string     `json:"kind"`
	Report   string     `json:"report"`
	At       time.Time  `json:"at"`
	Status   string     `json:"status"`
	Title    string     `json:"title"`
	Changed  string     `json:"changed"`
	Matters  string     `json:"matters"`
	Evidence string     `json:"evidence"`
	Next     string     `json:"next"`
	Body     *string    `json:"body"`
	Issue    *string    `json:"issue"`
	Pull     *ItemPull  `json:"pull"`
	Chart    *ItemChart `json:"chart"`
}

// ItemPull is the pull request a shipped item is about, with its verification's state now.
type ItemPull struct {
	Number int     `json:"number"`
	Title  *string `json:"title"`
	State  string  `json:"state"`
	Label  *string `json:"label"`
	URL    *string `json:"url"`
}

// ItemChart is the chart drawn for an item. Each row is a label, a value and whether the item is about it.
type ItemChart struct {
	Kind    string  `json:"kind"`
	Unit    string  `json:"unit"`
	Caption string  `json:"caption"`
	Rows    [][]any `json:"rows"`
}

// Overview reads what the analyst said about the app in the last day.
func (c Client) Overview(ctx context.Context, where Where) (Overview, error) {
	var overview Overview
	path := "/overview" + query(where.values())
	body, err := c.send(ctx, http.MethodGet, path, "", "application/json", nil)
	if err != nil {
		return Overview{}, err
	}
	if err := decode(c.BaseURL, body, &overview); err != nil {
		return Overview{}, err
	}
	overview.Raw = body
	if overview.Items == nil {
		overview.Items = []Item{}
	}
	if overview.Feed == nil {
		overview.Feed = []Item{}
	}
	return overview, nil
}
