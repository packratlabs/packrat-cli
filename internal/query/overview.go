package query

import (
	"context"
	"time"
)

// Overview is the answer of GET /overview: the items of an app's latest daily summary.
type Overview struct {
	App    string  `json:"app"`
	Org    string  `json:"org"`
	Report *Report `json:"report"`
	Items  []Item  `json:"items"`
}

// Report names the summary the items come from. Read the whole of it with packrat open.
type Report struct {
	ID      string    `json:"id"`
	Subject string    `json:"subject"`
	At      time.Time `json:"at"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

// Item is one thing the summary found worth a minute: act, watch, shipped or better.
type Item struct {
	Status   string     `json:"status"`
	Title    string     `json:"title"`
	Changed  string     `json:"changed"`
	Matters  string     `json:"matters"`
	Evidence string     `json:"evidence"`
	Next     string     `json:"next"`
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

// Overview reads the items of the app's latest daily summary.
func (c Client) Overview(ctx context.Context, where Where) (Overview, error) {
	var overview Overview
	if err := c.get(ctx, "/overview"+query(where.values()), &overview); err != nil {
		return Overview{}, err
	}
	if overview.Items == nil {
		overview.Items = []Item{}
	}
	return overview, nil
}
