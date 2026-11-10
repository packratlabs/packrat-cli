package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/query"
)

// Overview writes what the analyst said about an app in the last day, newest first.
func Overview(w io.Writer, o query.Overview, width int, queryURL string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s · what the analyst said in the last 24 hours\n", o.App)
	if len(o.Feed) == 0 {
		b.WriteString("\nNothing. The analyst writes a summary at 06:00, 12:00 and 18:00 Denver time.\n")
	}
	for _, item := range o.Feed {
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
		fmt.Fprintf(&b, "  %s · %s UTC\n", item.Report, item.At.UTC().Format("2006-01-02 15:04"))
		texts := []string{item.Changed, item.Matters, item.Evidence}
		if item.Next != "" {
			texts = append(texts, "Next: "+item.Next)
		}
		if item.Body != nil {
			texts = strings.Split(*item.Body, "\n\n")
		}
		for _, text := range texts {
			for _, line := range wrapWords(oneLine(text), max(width-2, 20)) {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
	}
	if len(o.Feed) > 0 {
		fmt.Fprintf(&b, "\nRead a whole mail with packrat open %s/reports/<id>\n", strings.TrimRight(queryURL, "/"))
	}
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
