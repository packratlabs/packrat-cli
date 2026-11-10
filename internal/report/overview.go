package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/query"
)

// Overview writes the items of an app's latest daily summary.
func Overview(w io.Writer, o query.Overview, width int, queryURL string) error {
	var b strings.Builder
	if o.Report == nil {
		fmt.Fprintf(&b, "=== %s\n\nNo summary yet. The analyst writes one at 06:00, 12:00 and 18:00 Denver time.\n", o.App)
		return put(w, b.String())
	}
	fmt.Fprintf(&b, "=== %s · %s\n%s · %s UTC\n", o.App, o.Report.Subject, o.Report.ID, o.Report.At.UTC().Format("2006-01-02 15:04"))
	if len(o.Items) == 0 {
		b.WriteString("\nNothing worth your minute in this summary.\n")
	}
	for _, item := range o.Items {
		tag := ""
		if item.Issue != nil {
			tag = "  " + *item.Issue
		}
		if item.Pull != nil {
			tag = "  #" + strconv.Itoa(item.Pull.Number)
			if item.Pull.Label != nil {
				tag += " " + *item.Pull.Label
			}
		}
		fmt.Fprintf(&b, "\n%s  %s%s\n", strings.ToUpper(item.Status), oneLine(item.Title), tag)
		next := ""
		if item.Next != "" {
			next = "Next: " + item.Next
		}
		for _, text := range []string{item.Changed, item.Matters, item.Evidence, next} {
			for _, line := range wrapWords(oneLine(text), max(width-2, 20)) {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
	}
	fmt.Fprintf(&b, "\nRead the whole summary with packrat open %s/reports/%s\n", strings.TrimRight(queryURL, "/"), o.Report.ID)
	return put(w, b.String())
}

// wrapWords breaks text into lines of at most width, between words.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// OverviewJSON writes the answer of the API as it was read.
func OverviewJSON(w io.Writer, o query.Overview) error { return putJSON(w, o) }
