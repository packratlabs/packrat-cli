package report

import (
	"fmt"
	"io"
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
const NotAnalysed = "The analyst is off for this app, so no deploy gets a verdict and no watch is read."

var verdicts = map[string]string{
	"confirmed": "worked", "partly": "worked in part", "refuted": "did not work",
	"too_early": "too early to say", "not_observable": "not observable", "": "not judged yet",
}

// Pull writes each deploy of a pull request with the analyst's verdict, then its watches.
func Pull(w io.Writer, p query.Pull, reports string) error {
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
	b.WriteString(watchLines(p.Watches, "No watch for this pull request."))
	return put(w, b.String())
}

// PullJSON writes the answer of the API as it was read.
func PullJSON(w io.Writer, p query.Pull) error { return putJSON(w, p) }

// Watches writes the open watches of an app, and the asked ones closed this week.
func Watches(w io.Writer, ws query.Watches) error {
	return put(w, "packrat watch list  "+ws.App+"\n\n"+watchLines(ws.Watches, "No watch is open."))
}

// WatchesJSON writes the answer of the API as it was read.
func WatchesJSON(w io.Writer, ws query.Watches) error { return putJSON(w, ws) }

// WatchJSON writes one watch as the API gave it.
func WatchJSON(w io.Writer, watch query.Watch) error { return putJSON(w, watch) }

func watchLines(watches []query.Watch, none string) string {
	if len(watches) == 0 {
		return none + "\n"
	}
	var b strings.Builder
	b.WriteString("Watches\n")
	for _, watch := range watches {
		state := "due " + stamp(watch.DueAt)
		if watch.ClosedAt != nil {
			state = "closed " + stamp(*watch.ClosedAt) + ": " + watch.Outcome
		}
		pull := ""
		if watch.Pull > 0 {
			pull = fmt.Sprintf(" #%d", watch.Pull)
		}
		fmt.Fprintf(&b, "  %s%s %s\n    %s\n", watch.ID, pull, state, firstLine(watch.What))
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

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}
