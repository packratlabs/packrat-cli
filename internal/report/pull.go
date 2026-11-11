package report

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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

// Pull writes the pull request's deploys, each with the analyst's verdict, then its checks, in the
// layout of heroku apps:info.
func Pull(w io.Writer, p query.Pull, reports string, width int) error {
	var b strings.Builder
	heading := fmt.Sprintf("=== #%d", p.Pull)
	for _, deploy := range p.Deploys {
		if deploy.Pull == p.Pull && deploy.Title != "" {
			heading += " " + deploy.Title
			break
		}
	}
	b.WriteString(heading + "\n" + p.App + "\n\n")
	switch {
	case len(p.Deploys) == 0 && p.Repository == "":
		b.WriteString(NotConnected + "\n\n")
	case len(p.Deploys) == 0:
		b.WriteString(NoDeploy + "\n\n")
	}
	if !p.Analysed {
		b.WriteString(NotAnalysed + "\n\n")
	}
	for _, deploy := range p.Deploys {
		fmt.Fprintf(&b, "Deploy    %s, live %s\nVerdict   %s\n", short(deploy.Version), stamp(deploy.DeployedAt), verdicts[deploy.Verdict])
		if deploy.Report != "" {
			fmt.Fprintf(&b, "Report    packrat open %s/reports/%s\n", reports, deploy.Report)
		}
		for _, part := range [][2]string{{"Asked for", deploy.Intent}, {"Evidence", deploy.Evidence}, {"Next", deploy.NextStep}} {
			if part[1] != "" {
				b.WriteString("\n" + part[0] + "\n" + fold(part[1], width-2, "  "))
			}
		}
		b.WriteString("\n")
	}
	if len(p.Declared) > 0 {
		b.WriteString("=== Waiting for the deploy\n")
		b.WriteString(declaredTable(p.Declared, width) + "\n")
	}
	if len(p.Dropped) > 0 {
		b.WriteString("=== Not declared\n")
		for _, check := range p.Dropped {
			b.WriteString(cut(check.Title+": "+check.Dropped, max(width, 20)) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("=== Checks\n")
	b.WriteString(checkTable(p.Checks, false, "No check for this pull request.", width))
	return put(w, b.String())
}

// Declaration writes what a pull request declared, or what a dry run found in a draft.
func Declaration(w io.Writer, d query.Declaration, width int) error {
	var b strings.Builder
	count := fmt.Sprintf("%d checks", len(d.Checks))
	if len(d.Checks) == 1 {
		count = "1 check"
	}
	switch {
	case d.DryRun && d.None != "":
		fmt.Fprintf(&b, "The draft declares no checks for %s: %s\n", d.App, d.None)
	case d.DryRun:
		fmt.Fprintf(&b, "The draft holds %s for %s.\n", count, d.App)
	case d.None != "":
		fmt.Fprintf(&b, "#%d declares no checks in %s: %s\n", d.Pull, d.App, d.None)
	default:
		fmt.Fprintf(&b, "Declared %s for #%d in %s. Each runs every five minutes from the deploy that ships it until its window ends.\n", count, d.Pull, d.App)
	}
	if len(d.Checks) > 0 {
		b.WriteString("\n" + declaredTable(d.Checks, width))
	}
	for _, warning := range d.Warnings {
		b.WriteString("\nWarning: " + warning + "\n")
	}
	return put(w, b.String())
}

// DeclarationJSON writes the answer of the API as it was read.
func DeclarationJSON(w io.Writer, d query.Declaration) error { return putJSON(w, d) }

func declaredTable(checks []query.Declared, width int) string {
	var b strings.Builder
	b.WriteString("WINDOW TITLE\n")
	for _, check := range checks {
		window := check.Window
		if check.Dropped != "" {
			window = "—"
		}
		line := fmt.Sprintf("%-6s %s", window, check.Title)
		if width > 0 && len([]rune(line)) > width {
			line = string([]rune(line)[:width-1]) + "…"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// PullJSON writes the answer of the API as it was read.
func PullJSON(w io.Writer, p query.Pull) error { return putJSON(w, p) }

// Checks writes the open checks of an app, and the asked ones closed this week.
func Checks(w io.Writer, ws query.Checks, width int) error {
	return put(w, "=== "+ws.App+" checks\n\n"+checkTable(ws.Checks, true, "No check is open, and none closed this week.", width))
}

// ChecksJSON writes the answer of the API as it was read.
func ChecksJSON(w io.Writer, ws query.Checks) error { return putJSON(w, ws) }

// CheckJSON writes one check as the API gave it.
func CheckJSON(w io.Writer, check query.Check) error { return putJSON(w, check) }

// CheckTable writes checks one to a line: open ones first, soonest due, then closed ones, newest
// first. What is cut to fit width; packrat check show gives the whole text.
func checkTable(checks []query.Check, withPull bool, none string, width int) string {
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
	pullWidth := 0
	if withPull {
		pullWidth = 5 + 2
	}
	whatWidth := max(width-(pullWidth+status+2+2+idWidth), 20)
	var b strings.Builder
	if withPull {
		fmt.Fprintf(&b, "%-5s  ", "PR")
	}
	fmt.Fprintf(&b, "%-*s  %-*s  %s\n", status, "STATUS", whatWidth, "WHAT", "ID")
	for _, check := range rows {
		if withPull {
			pull := "—"
			if check.Pull > 0 {
				pull = fmt.Sprintf("#%d", check.Pull)
			}
			fmt.Fprintf(&b, "%-5s  ", pull)
		}
		state := openState(check)
		if check.ClosedAt != nil {
			state = "closed " + day(*check.ClosedAt)
		}
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n", status, state, whatWidth, cut(oneLine(withoutPull(check)), whatWidth), check.ID)
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
	} else if check.Kind != "" {
		head = append(head, progress(check)+" · window ends "+stamp(check.DueAt))
	} else {
		head = append(head, "due "+stamp(check.DueAt))
	}
	b.WriteString(strings.Join(head, " · ") + "\n\nAsked\n" + fold(check.What, width-2, "  "))
	if check.Outcome != "" {
		b.WriteString("\nResult\n" + fold(check.Outcome, width-2, "  "))
	}
	return put(w, b.String())
}

// openState is an open check's STATUS: how far a rule check has got, or when a check from before rules is due.
func openState(check query.Check) string {
	if check.Kind == "" {
		return "due " + day(check.DueAt)
	}
	if check.Review != "" || check.Seen != nil {
		return progress(check)
	}
	return "ends " + day(check.DueAt)
}

// progress is how far a rule check has got: "seen 40/200", or that it waits for review.
func progress(check query.Check) string {
	switch {
	case check.Review != "":
		return "to review"
	case check.Seen != nil:
		return fmt.Sprintf("seen %d/%d", *check.Seen, check.MinSeen)
	default:
		return "not run yet"
	}
}

// pullPrefix is how the analyst starts a check's text, "PR #542 (39428589, web live 17:44):", which the
// table's PR column already says.
var pullPrefix = regexp.MustCompile(`^\s*PR #(\d+)\s*(\([^)]*\))?[,:]?\s*`)

// withoutPull is a check's text without that start, when it names the check's own pull request.
func withoutPull(check query.Check) string {
	found := pullPrefix.FindStringSubmatchIndex(check.What)
	if found == nil || check.What[found[2]:found[3]] != strconv.Itoa(check.Pull) {
		return check.What
	}
	return check.What[found[1]:]
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

// SignedIn says who the key is, the orgs it opens, and which of them commands read here (here is
// its public id, or empty when commands must name one), and why.
func SignedIn(w io.Writer, who query.Who, fromEnv bool, here, why string) error {
	var b strings.Builder
	from := "packrat login"
	if fromEnv {
		from = query.KeyName + ", which is used before any sign-in"
	}
	person := who.Email
	if person == "" {
		person = "an org (no person)"
	}
	fmt.Fprintf(&b, "Signed in as %s.\nKey %s, %q, from %s.\n\nOrgs\n", person, who.Key, who.Name, from)
	width := 0
	for _, org := range who.Orgs {
		width = max(width, len(org.Name))
	}
	for _, org := range who.Orgs {
		mark := " "
		if org.Org == here {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %-*s  %s\n", mark, width, org.Name, org.Org)
	}
	if here != "" {
		fmt.Fprintf(&b, "\n* is the org commands read here: %s.\n", why)
	} else if len(who.Orgs) > 1 {
		b.WriteString("\nCommands here find the org from --app, or name it with --org or PACKRAT_ORG.\n")
	}
	return put(w, b.String())
}

func short(version string) string { return version[:min(8, len(version))] }

func stamp(at time.Time) string { return at.UTC().Format("2006-01-02 15:04 UTC") }

// PullArchived says where a pull request's verification stands after a member archived it or moved it back.
func PullArchived(w io.Writer, p query.Pull) error {
	line := fmt.Sprintf("Moved #%d in %s back to pending, with its checks open again.", p.Pull, p.App)
	if p.State == "archived" {
		line = fmt.Sprintf("Archived #%d in %s and cancelled its open checks.", p.Pull, p.App)
	}
	if p.ArchiveReason != "" {
		line += "\nReason: " + p.ArchiveReason
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("writing the answer: %w", err)
	}
	return nil
}
