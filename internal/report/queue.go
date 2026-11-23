package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/query"
)

// Endings are the words a person reads for each ending a loop reports.
var Endings = map[string]string{
	"fix": "fix", "fixed_already": "fixed already", "excuse": "excuse the check", "replace": "replace the check", "needs_person": "needs a person",
}

// Queue writes the pull requests that wait for a loop, then the ones a loop holds.
func Queue(w io.Writer, q query.Queue) error {
	var b strings.Builder
	b.WriteString("=== " + q.App + " queue\n\n")
	if len(q.Waiting) == 0 && len(q.Taken) == 0 {
		b.WriteString("No failed pull request waits for a loop.\n")
		return put(w, b.String())
	}
	rows := [][]string{{"PR", "VERDICT", "STATUS", "TITLE"}}
	for _, item := range q.Waiting {
		rows = append(rows, []string{fmt.Sprintf("#%d", item.Number), verdicts[item.Verdict], "waiting", item.Title})
	}
	for _, item := range q.Taken {
		status := "taken"
		if item.TakenAt != nil {
			status = "taken " + stamp(*item.TakenAt)
		}
		rows = append(rows, []string{fmt.Sprintf("#%d", item.Number), verdicts[item.Verdict], status, item.Title})
	}
	widths := make([]int, 3)
	for _, row := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", widths[0], row[0], widths[1], row[1], widths[2], row[2], row[3])
	}
	return put(w, b.String())
}

// QueueJSON writes the answer of the API as it was read.
func QueueJSON(w io.Writer, q query.Queue) error { return putJSON(w, q) }

// Item writes the pull request a loop took: what failed, said as its page says it, and the issues it left open.
func Item(w io.Writer, item query.Item, width int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "=== #%d %s\n%s\n\nDeploy    %s, live %s\nVerdict   %s\n", item.Number, item.Title, item.App, short(item.Version), stamp(item.DeployedAt), verdicts[item.Verdict])
	for _, check := range item.FailedChecks {
		fmt.Fprintf(&b, "\nFailed    %s  %s\n", check.ID, check.Title)
		for _, part := range [][2]string{{"Claim", check.Claim}, {"Expected", check.Expected}, {"Instead", check.Instead}, {"Outcome", check.Outcome}} {
			if part[1] != "" {
				b.WriteString("\n" + part[0] + "\n" + fold(part[1], width-2, "  "))
			}
		}
	}
	if len(item.Issues) > 0 {
		b.WriteString("\nIssues\n")
		for _, issue := range item.Issues {
			fmt.Fprintf(&b, "  %s  %s, %s\n", issue.ID, issue.Link, issue.Status)
		}
	}
	fmt.Fprintf(&b, "\nReport how it ends with packrat queue done %d. Run packrat help queue done for the endings.\n", item.Number)
	return put(w, b.String())
}

// NextJSON writes the answer of the API as it was read.
func NextJSON(w io.Writer, next query.Next) error { return putJSON(w, next) }

// Changed is what an ending changed, as the log and the answer say it: "excused chk_1 · added chk_2".
func Changed(e query.Ended) string {
	var parts []string
	if e.Fix != "" {
		parts = append(parts, e.Fix)
	}
	if e.Excused != "" {
		parts = append(parts, "excused "+e.Excused)
	}
	if e.Added != "" {
		parts = append(parts, "added "+e.Added)
	}
	if e.Excused != "" {
		parts = append(parts, "now "+state(e.State))
	}
	return strings.Join(parts, " · ")
}

// Ended says how a pull request ended and what that changed.
func Ended(w io.Writer, e query.Ended) error {
	line := fmt.Sprintf("Ended #%d in %s: %s.", e.Pull, e.App, Endings[e.Ending])
	if changed := Changed(e); changed != "" {
		line += "\n" + strings.ToUpper(changed[:1]) + changed[1:] + "."
	}
	return put(w, line+"\n")
}

// EndedJSON writes the answer of the API as it was read.
func EndedJSON(w io.Writer, e query.Ended) error { return putJSON(w, e) }

func state(name string) string {
	if name == "pending" || name == "archived" {
		return name
	}
	return verdicts[name]
}
