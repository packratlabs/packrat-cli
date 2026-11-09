package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/query"
)

// IssueList writes the app's issues one to a line, in the order the API gave them.
func IssueList(w io.Writer, list query.Issues, status string, width int) error {
	if status == "" {
		status = "unresolved"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s issues · %s, events over %s\n\n", list.App, status, list.Period)
	if len(list.Issues) == 0 {
		b.WriteString("No issue.\n")
		return put(w, b.String())
	}
	idWidth := len("ID")
	for _, issue := range list.Issues {
		idWidth = max(idWidth, len(issue.ID))
	}
	const severityWidth, countWidth = len("SEVERITY"), len("EVENTS")
	titleWidth := max(width-(idWidth+severityWidth+countWidth+len("USERS")+4*2), 20)
	row := func(id, severity, events, users, title string) {
		fmt.Fprintf(&b, "%-*s  %-*s  %*s  %*s  %s\n", idWidth, id, severityWidth, severity, countWidth, events, len("USERS"), users, title)
	}
	row("ID", "SEVERITY", "EVENTS", "USERS", "TITLE")
	for _, issue := range list.Issues {
		severity := issue.Severity
		if severity == "" {
			severity = "—"
		}
		title := oneLine(issue.Title)
		if issue.Kind == "detected" {
			title = "AI: " + title
		}
		row(issue.ID, severity, strconv.Itoa(issue.Events), strconv.Itoa(issue.Users), cut(title, titleWidth))
	}
	return put(w, b.String())
}

// IssueJSON writes one issue as the API answered it.
func IssueJSON(w io.Writer, issue query.Issue) error { return putJSON(w, issue) }

// IssueListJSON writes the answer of the API as it was read.
func IssueListJSON(w io.Writer, list query.Issues) error { return putJSON(w, list) }

// IssueActed says what became of an issue a member resolved, archived or reopened.
func IssueActed(w io.Writer, issue query.Issue) error {
	var line string
	switch {
	case issue.Status == "resolved":
		line = "Resolved " + issue.ID + "."
	case issue.Status == "archived" && issue.ArchivedUntil == "forever":
		line = "Archived " + issue.ID + " for good."
	case issue.Status == "archived":
		line = "Archived " + issue.ID + " until it escalates."
	default:
		line = "Reopened " + issue.ID + "."
	}
	if issue.Reason != "" {
		line += "\nReason: " + issue.Reason
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("writing the answer: %w", err)
	}
	return nil
}
