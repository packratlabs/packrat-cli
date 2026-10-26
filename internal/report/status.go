package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/packratlabs/packrat-cli/internal/query"
)

// NothingYet is printed for a signal that no source sends.
const NothingYet = "nothing yet"

// FirstSource is printed when a source sends a signal that the app keeps from another source.
const FirstSource = "  Packrat keeps the first source of exceptions and of traces.\n  To use a different source, ask us to switch it."

// NoApps is printed when the key opens no app.
const NoApps = "No data has arrived for this org.\n\nStart the app and load a page. Then run: packrat apps"

// Fresh is how recently data must have arrived for an app to count as sending.
const Fresh = time.Hour

// A source's time is written at most once a minute, so a younger age is not known.
const exact = 2 * time.Minute

var signalLabels = map[string]string{
	"exceptions": "Exceptions", "traces": "Traces", "logs": "Logs", "metrics": "Metrics", "replays": "Replays",
}

// Apps writes one line for each app the key opens.
func Apps(w io.Writer, apps query.Apps) error {
	if len(apps.Apps) == 0 {
		return put(w, NoApps+"\n")
	}
	width := len("APP")
	for _, app := range apps.Apps {
		width = max(width, len(app.App))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %s\n", width, "APP", "LAST DATA")
	for _, app := range apps.Apps {
		last := "none"
		if app.LastDataAt != nil {
			last = ago(apps.Now.Sub(*app.LastDataAt))
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, app.App, last)
	}
	return put(w, b.String())
}

// Status writes each signal of one app, with a line for each source that sends it.
func Status(w io.Writer, s query.Status) error {
	var b strings.Builder
	b.WriteString("packrat status  " + s.App.App + "\n\n")
	unkept, connected := false, 0
	width := len("Datadog browser")
	for _, signal := range s.Signals {
		for _, source := range signal.Sources {
			width = max(width, len(source.Name))
		}
	}
	fmt.Fprintf(&b, "  %-10s  %-*s  %-4s  %s\n", "SIGNAL", width, "SOURCE", "KEPT", "LAST DATA")
	for _, signal := range s.Signals {
		label := signalLabel(signal.Signal)
		if len(signal.Sources) == 0 {
			fmt.Fprintf(&b, "  %-10s  %s\n", label, NothingYet)
			continue
		}
		connected++
		for i, source := range signal.Sources {
			if i > 0 {
				label = ""
			}
			unkept = unkept || !source.Kept
			line := fmt.Sprintf("  %-10s  %-*s  %-4s  %s", label, width, source.Name, kept(source), seen(s, signal, source))
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
	for _, signal := range s.Signals {
		if signal.Quiet {
			b.WriteString("\n" + quiet(s, signal) + "\n")
		}
	}
	if unkept {
		b.WriteString("\n" + FirstSource + "\n")
	}
	b.WriteString("\n" + statusSummary(s, connected) + "\n")
	return put(w, b.String())
}

// Statuses writes the status of each app, with a blank line between two apps.
func Statuses(w io.Writer, statuses []query.Status) error {
	for i, s := range statuses {
		if i > 0 {
			if err := put(w, "\n"); err != nil {
				return err
			}
		}
		if err := Status(w, s); err != nil {
			return err
		}
	}
	return nil
}

// StatusesJSON writes the answers of the API for several apps as one list.
func StatusesJSON(w io.Writer, statuses []query.Status) error {
	return putJSON(w, statuses)
}

// StatusJSON writes the answer of the API as it was read.
func StatusJSON(w io.Writer, s query.Status) error {
	return putJSON(w, s)
}

// AppsJSON writes the answer of the API as it was read.
func AppsJSON(w io.Writer, apps query.Apps) error {
	return putJSON(w, apps)
}

// Sending is false when nothing arrived within Fresh, or when a kept source stopped while another
// source sends. It decides the exit status of packrat status.
func Sending(s query.Status) bool {
	for _, signal := range s.Signals {
		if signal.Quiet {
			return false
		}
	}
	last := Last(s)
	return last != nil && s.Now.Sub(*last) <= Fresh
}

// Last is the newest time anything arrived for the app, or nil when nothing did.
func Last(s query.Status) *time.Time {
	last := s.LastDataAt
	for _, signal := range s.Signals {
		for _, source := range signal.Sources {
			if source.LastSeenAt != nil && (last == nil || source.LastSeenAt.After(*last)) {
				last = source.LastSeenAt
			}
		}
	}
	return last
}

// statusSummary counts a signal with a source as connected, whether or not that source has sent yet.
func statusSummary(s query.Status, connected int) string {
	last := Last(s)
	switch {
	case last == nil:
		return "No data has arrived for this app."
	case s.Now.Sub(*last) > Fresh:
		return "Nothing has arrived for " + span(s.Now.Sub(*last)) + "."
	case connected == 1:
		return fmt.Sprintf("1 of %d signals is connected.", len(s.Signals))
	default:
		return fmt.Sprintf("%d of %d signals are connected.", connected, len(s.Signals))
	}
}

func quiet(s query.Status, signal query.Signal) string {
	var keptName, keptAge string
	var others []string
	for _, source := range signal.Sources {
		switch {
		case source.Kept && keptName == "":
			keptName = source.Name
			keptAge = "has sent nothing"
			if source.LastSeenAt != nil {
				keptAge = "last sent " + ago(s.Now.Sub(*source.LastSeenAt))
			}
		case !source.Kept:
			others = append(others, source.Name)
		}
	}
	return fmt.Sprintf("  %s come from %s, which %s. %s sends them now, and they are not kept.\n  Ask us to switch the source.",
		signalLabel(signal.Signal), keptName, keptAge, strings.Join(others, " and "))
}

func signalLabel(name string) string {
	if label, ok := signalLabels[name]; ok {
		return label
	}
	return name
}

func kept(source query.Source) string {
	if source.Kept {
		return "yes"
	}
	return "no"
}

func seen(s query.Status, signal query.Signal, source query.Source) string {
	if source.LastSeenAt == nil {
		return "no " + signal.Signal + " yet"
	}
	return ago(s.Now.Sub(*source.LastSeenAt))
}

func ago(age time.Duration) string {
	if age < exact {
		return "now"
	}
	return span(age) + " ago"
}

func span(age time.Duration) string {
	switch {
	case age < time.Hour:
		return count(int(age/time.Minute), "minute")
	case age < 48*time.Hour:
		return count(int(age/time.Hour), "hour")
	default:
		return count(int(age/(24*time.Hour)), "day")
	}
}

func count(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func put(w io.Writer, text string) error {
	if _, err := io.WriteString(w, text); err != nil {
		return fmt.Errorf("writing the answer: %w", err)
	}
	return nil
}

func putJSON(w io.Writer, value any) error {
	doc, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the answer: %w", err)
	}
	return put(w, string(doc)+"\n")
}
