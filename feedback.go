package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/query"
)

var (
	feedbackKinds   = []string{"missing", "broken", "confusing", "idea"}
	feedbackImpacts = []string{"blocked", "worked_around", "friction"}
)

func runFeedback(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("feedback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var feedback query.Feedback
	fs.StringVar(&feedback.Task, "task", "", "")
	fs.StringVar(&feedback.Kind, "kind", "", "")
	fs.StringVar(&feedback.Impact, "impact", "", "")
	fs.StringVar(&feedback.About, "about", "", "")
	fs.StringVar(&feedback.App, "app", w.getenv("PACKRAT_APP"), "")
	words, err := parseInterleaved(fs, args)
	switch {
	case err != nil || len(words) > 0:
		return say(stderr, "packrat feedback: specify only flags, and the description on stdin. Run packrat help feedback for details.\n", exitUsage)
	case !slices.Contains(feedbackKinds, feedback.Kind):
		return say(stderr, "packrat feedback: --kind is one of "+strings.Join(feedbackKinds, ", ")+"\n", exitUsage)
	case !slices.Contains(feedbackImpacts, feedback.Impact):
		return say(stderr, "packrat feedback: --impact is one of "+strings.Join(feedbackImpacts, ", ")+"\n", exitUsage)
	case strings.TrimSpace(feedback.Task) == "":
		return say(stderr, "packrat feedback: --task names what you were doing, in a short phrase\n", exitUsage)
	}
	description, err := io.ReadAll(w.stdin)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat feedback: reading the description: %v\n", err), exitFailed)
	}
	feedback.Description = strings.TrimSpace(string(description))
	if feedback.Description == "" {
		return say(stderr, "packrat feedback: provide the description on stdin: what you ran, what happened and what you expected\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat feedback: %v\n", err), exitUsage)
	}
	sent, err := siteClient(w, client.Key).In(client.Org, client.Repo).SendFeedback(context.Background(), feedback)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat feedback: %v\n", err), exitFailed)
	}
	return say(stdout, sent+"\n", exitOK)
}
