package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runKeys makes an org's API key, for an agent that runs where nobody can sign in. It is the one command that
// prints a key: --plain prints it alone, for $(…), so it can reach a platform's config without passing through a
// terminal or an agent's transcript.
func runKeys(w world, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	plain := fs.Bool("plain", false, "")
	words, err := parseInterleaved(fs, args)
	switch {
	case err != nil || len(words) != 1 || words[0] != "create":
		return say(stderr, "packrat keys: the command is packrat keys create --name NAME. Run packrat help keys create for details.\n", exitUsage)
	case strings.TrimSpace(*name) == "":
		return say(stderr, "packrat keys create: --name says what the key is for, such as \"deploy agent\". The org's keys page lists keys by name.\n", exitUsage)
	}
	client, err := newClient(w)
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat keys create: %v\n", err), exitUsage)
	}
	made, err := siteClient(w, client.Key).In(client.Org, client.Repo).CreateKey(context.Background(), strings.TrimSpace(*name))
	if err != nil {
		return say(stderr, fmt.Sprintf("packrat keys create: %v\n", err), exitFailed)
	}
	if *plain {
		return say(stdout, made.Token+"\n", exitOK)
	}
	return say(stdout, fmt.Sprintf("Made the API key %q in %s.\n\n  %s\n\n"+
		"This is the only time it's shown. Set it as PACKRAT_API_KEY where your agent runs.\n"+
		"It reads every app in the org. Revoke it at %s/orgs/%s/keys.\n",
		made.Name, made.OrgName, made.Token, siteURL(w), made.Org), exitOK)
}
