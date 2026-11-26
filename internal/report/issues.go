package report

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

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

// framesShown is how many frames of an exception IssueShow prints, innermost first.
const framesShown = 8

// IssueShow writes one issue: what it is, where it stands, how often it happened, and its latest event.
func IssueShow(w io.Writer, issue query.IssueDetail, width int) error {
	var b strings.Builder
	title := oneLine(issue.Title)
	if issue.Kind == "detected" {
		title = "AI: " + title
	}
	b.WriteString(cut("=== "+issue.ID+" "+title, max(width, 20)) + "\n")
	status := issue.Status
	if issue.Substatus != "" && issue.Substatus != issue.Status {
		status += ", " + issue.Substatus
	}
	if issue.Status == "archived" && issue.ArchivedUntil != "" {
		status += " until " + map[string]string{"escalating": "it escalates", "forever": "forever"}[issue.ArchivedUntil]
	}
	severity := issue.Severity
	if severity == "" {
		severity = "unrated"
	}
	rows := [][2]string{
		{"Status", status},
		{"Severity", severity},
		{"First seen", seenAt(issue.FirstSeen, issue.FirstRelease)},
		{"Last seen", seenAt(issue.LastSeen, issue.LastRelease)},
	}
	if issue.Kind != "detected" {
		rows = append(rows[:2], append([][2]string{{"Events", fmt.Sprintf("%d from %d %s", issue.Events, issue.Users, plural(issue.Users, "user"))}}, rows[2:]...)...)
	}
	if issue.Culprit != "" {
		rows = append(rows, [2]string{"Where", issue.Culprit})
	}
	if issue.PartOf != "" {
		rows = append(rows, [2]string{"Part of", issue.PartOf})
	}
	if len(issue.Errors) > 0 {
		rows = append(rows, [2]string{"Errors", strings.Join(issue.Errors, ", ")})
	}
	if issue.Pull != 0 {
		rows = append(rows, [2]string{"Pull", fmt.Sprintf("#%d (packrat pr show %d)", issue.Pull, issue.Pull)})
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "%-11s %s\n", row[0], row[1])
	}
	for _, part := range [][2]string{{"Summary", issue.Summary}, {"Normal", issue.Normal}, {"Reason", issue.Reason}} {
		if part[1] != "" {
			b.WriteString("\n" + part[0] + "\n" + fold(part[1], width-2, "  "))
		}
	}
	if issue.Event != nil {
		b.WriteString("\n" + eventText(*issue.Event, width))
	}
	if len(issue.Tags) > 0 {
		b.WriteString("\n=== Tags (commonest value)\n")
		keys := make([]string, 0, len(issue.Tags))
		for key := range issue.Tags {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteString(cut(fmt.Sprintf("%-20s %s (%d)", key, issue.Tags[key].Top, issue.Tags[key].Count), max(width, 20)) + "\n")
		}
	}
	return put(w, b.String())
}

func eventText(event query.IssueEvent, width int) string {
	var b strings.Builder
	b.WriteString("=== Latest event " + event.EventID + "\n")
	fmt.Fprintf(&b, "%s", stamp(event.OccurredAt))
	for _, part := range []string{short(event.Release), event.Transaction} {
		if part != "" {
			b.WriteString(" · " + part)
		}
	}
	b.WriteString("\n")
	if event.TraceID != "" {
		b.WriteString("Trace " + event.TraceID + "\n")
	}
	for i := len(event.Exceptions) - 1; i >= 0; i-- {
		exception := event.Exceptions[i]
		b.WriteString("\n" + fold(strings.TrimSpace(exception.Type+": "+exception.Value), width, ""))
		frames := exception.Frames
		shown := 0
		for j := len(frames) - 1; j >= 0 && shown < framesShown; j-- {
			frame := frames[j]
			if frame.InApp != nil && !*frame.InApp {
				continue
			}
			line := "  " + frame.Filename
			if frame.Lineno > 0 {
				line += ":" + strconv.Itoa(frame.Lineno)
			}
			if frame.Function != "" {
				line += " in " + frame.Function
			}
			b.WriteString(cut(line, max(width, 20)) + "\n")
			shown++
		}
	}
	return b.String()
}

func seenAt(at time.Time, release string) string {
	if at.IsZero() {
		return "—"
	}
	if release == "" {
		return stamp(at)
	}
	return stamp(at) + " in " + short(release)
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// IssueJSON writes one issue as the API answered it.
func IssueJSON(w io.Writer, issue query.Issue) error { return putJSON(w, issue) }

// IssueDetailJSON writes one issue and its latest event as the API answered them.
func IssueDetailJSON(w io.Writer, issue query.IssueDetail) error { return putJSON(w, issue) }

// SeenJSON writes the answer of a PUT …/seen as it was read.
func SeenJSON(w io.Writer, answer any) error { return putJSON(w, answer) }

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
