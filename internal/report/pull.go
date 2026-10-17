package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
	"github.com/packratlabs/packrat-cli/internal/query"
)

// NoDeploy is printed for a pull request that no deploy has shipped yet.
const NoDeploy = "No deploy has shipped it yet."

// NotConnected is printed when the app's repository is not connected, so no deploy has a pull request.
const NotConnected = "The app's GitHub repository is not connected in Packrat, so no deploy names a pull request. Connect it on the app's Analyst page."

// NotAnalysed is printed when the analyst does not look at the app.
const NotAnalysed = "The analyst is off for this app, so no deploy gets a verdict and no check is read."

var verdicts = map[string]string{
	"confirmed": "worked", "partly": "worked in part", "refuted": "did not work",
	"too_early": "too early to say", "not_observable": "not observable", "": "not judged yet",
}

// Pull writes each deploy of a pull request with the analyst's verdict, then its checks.
func Pull(w io.Writer, p query.Pull, reports string, width int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "packrat pr  #%d  %s\n\n", p.Pull, p.App)
	switch {
	case len(p.Deploys) == 0 && p.Repository == "":
		b.WriteString(NotConnected + "\n")
	case len(p.Deploys) == 0:
		b.WriteString(NoDeploy + "\n")
	}
	if !p.Analysed {
		b.WriteString(NotAnalysed + "\n")
	}
	for _, deploy := range p.Deploys {
		title := ""
		if deploy.Pull == p.Pull && deploy.Title != "" {
			title = "  " + deploy.Title
		}
		fmt.Fprintf(&b, "Deploy %s, live %s: %s%s\n", short(deploy.Version), stamp(deploy.DeployedAt), verdicts[deploy.Verdict], title)
		for _, line := range [][2]string{{"Asked for", deploy.Intent}, {"Evidence", deploy.Evidence}, {"Next", deploy.NextStep}} {
			if line[1] != "" {
				fmt.Fprintf(&b, "  %s: %s\n", line[0], line[1])
			}
		}
		if deploy.Report != "" {
			fmt.Fprintf(&b, "  Report: packrat open %s/reports/%s\n", reports, deploy.Report)
		}
		b.WriteString("\n")
	}
	if len(p.Checks) > 0 {
		b.WriteString("Checks\n\n")
	}
	b.WriteString(checkTable(p.Checks, "No check for this pull request.", width))
	return put(w, b.String())
}

// PullJSON writes the answer of the API as it was read.
func PullJSON(w io.Writer, p query.Pull) error { return putJSON(w, p) }

// Checks writes the open checks of an app, and the asked ones closed this week.
func Checks(w io.Writer, ws query.Checks, width int) error {
	return put(w, "=== "+ws.App+" checks\n\n"+checkTable(ws.Checks, "No check is open, and none closed this week.", width))
}

// ChecksJSON writes the answer of the API as it was read.
func ChecksJSON(w io.Writer, ws query.Checks) error { return putJSON(w, ws) }

// CheckJSON writes one check as the API gave it.
func CheckJSON(w io.Writer, check query.Check) error { return putJSON(w, check) }

// CheckTable writes checks one to a line: open ones first, soonest due, then closed ones, newest
// first. What is cut to fit width; packrat check show gives the whole text.
func checkTable(checks []query.Check, none string, width int) string {
	if len(checks) == 0 {
		return none + "\n"
	}
	rows := append([]query.Check(nil), checks...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.ClosedAt == nil) != (b.ClosedAt == nil) {
			return a.ClosedAt == nil
		}
		if a.ClosedAt == nil {
			return a.DueAt.Before(b.DueAt)
		}
		return a.ClosedAt.After(*b.ClosedAt)
	})
	const status = len("closed 15 Oct 15:07")
	idWidth := len("ID")
	for _, check := range rows {
		idWidth = max(idWidth, len(check.ID))
	}
	whatWidth := max(width-(5+2+status+2+2+idWidth), 20)
	var b strings.Builder
	fmt.Fprintf(&b, "%-5s  %-*s  %-*s  %s\n", "PR", status, "STATUS", whatWidth, "WHAT", "ID")
	for _, check := range rows {
		pull := "—"
		if check.Pull > 0 {
			pull = fmt.Sprintf("#%d", check.Pull)
		}
		state := "due " + day(check.DueAt)
		if check.ClosedAt != nil {
			state = "closed " + day(*check.ClosedAt)
		}
		fmt.Fprintf(&b, "%-5s  %-*s  %-*s  %s\n", pull, status, state, whatWidth, cut(oneLine(check.What), whatWidth), check.ID)
	}
	b.WriteString("\nTimes are UTC. packrat check show <ID> prints the whole text.\n")
	return b.String()
}

// Check writes one check in full: what was asked and, once it closed, what the data showed.
func Check(w io.Writer, check query.Check, width int) error {
	var b strings.Builder
	head := []string{check.ID}
	if check.Pull > 0 {
		head = append(head, fmt.Sprintf("PR #%d", check.Pull))
	}
	if check.ClosedAt != nil {
		head = append(head, "closed "+stamp(*check.ClosedAt))
	} else {
		head = append(head, "due "+stamp(check.DueAt))
	}
	b.WriteString(strings.Join(head, " · ") + "\n\nAsked\n" + fold(check.What, width-2, "  "))
	if check.Outcome != "" {
		b.WriteString("\nResult\n" + fold(check.Outcome, width-2, "  "))
	}
	return put(w, b.String())
}

func day(at time.Time) string { return at.UTC().Format("2 Jan 15:04") }

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func cut(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width-1]) + "…"
}

// fold breaks each paragraph of text at spaces to fit width, and indents every line.
func fold(text string, width int, indent string) string {
	var b strings.Builder
	for _, paragraph := range strings.Split(strings.TrimSpace(text), "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
				b.WriteString(indent + line + "\n")
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		b.WriteString(strings.TrimRight(indent+line, " ") + "\n")
	}
	return b.String()
}

// SignedIn writes who a key is, where it came from, and the other sign-ins kept.
func SignedIn(w io.Writer, who query.Who, fromEnv bool, current string, all []auth.SignIn) error {
	var b strings.Builder
	from := "packrat login"
	if fromEnv {
		from = query.KeyName + ", which is used before any sign-in"
	}
	person := who.Email
	if person == "" {
		person = "the org (no person)"
	}
	fmt.Fprintf(&b, "Signed in to %s (%s) as %s.\nKey %s, %q, from %s.\n", who.OrgName, who.Org, person, who.Key, who.Name, from)
	if len(all) > 1 {
		b.WriteString("\nSign-ins kept:\n")
		for _, signIn := range all {
			mark := " "
			if signIn.Org == current {
				mark = "*"
			}
			fmt.Fprintf(&b, "  %s %s (%s) as %s\n", mark, signIn.OrgName, signIn.Org, signIn.Email)
		}
	}
	return put(w, b.String())
}

func short(version string) string { return version[:min(8, len(version))] }

func stamp(at time.Time) string { return at.UTC().Format("2006-01-02 15:04 UTC") }
