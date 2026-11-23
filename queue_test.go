package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

const queueItem = `{"kind":"pull","number":482,"title":"Cache the cart total","app":"shop/production","repository":"acme/shop","verdict":"partly",` +
	`"deployed_at":"2026-10-14T16:02:00Z","version":"9f2c41e8aa","judged_at":"2026-10-15T16:05:00Z","evidence":"The cart is faster failed: p95 410 ms.","taken_at":null,` +
	`"issues":[{"id":"SHOP-41","link":"opened","status":"unresolved"}],` +
	`"failed_checks":[{"id":"chk_1","title":"The cart is faster","claim":"The p95 of GET /cart is under 300 ms.","expected":"The cart loads in under 300 ms.",` +
	`"instead":"It took 410 ms.","outcome":"seen 900, p95_ms 410","source":"follow_up","sql":"select 1","fail_when":"p95_ms >= 300"}]}`

// fakeQueue answers /queue the way query.packrat.sh does. The app named picks what the queue holds: "later" is empty
// twice before it has a pull request, and "flaky" fails once in between.
func fakeQueue(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request: %v", err)
		}
		asked = append(asked, r.Method+" "+r.URL.RequestURI()+" "+strings.TrimSpace(string(body)))
		answer := func(code int, text string) {
			w.WriteHeader(code)
			if _, err := w.Write([]byte(text)); err != nil {
				t.Errorf("answering: %v", err)
			}
		}
		app := r.URL.Query().Get("app")
		empty := map[string]string{"empty": `{"app":"empty/production","item":null,"waiting":0}`, "later": `{"app":"later/production","item":null,"waiting":0}`,
			"flaky": `{"app":"flaky/production","item":null,"waiting":0}`}[app]
		taken := `{"app":"shop/production","item":` + queueItem + `,"waiting":1}`
		switch {
		case r.Header.Get("Authorization") != "Bearer "+loginKey:
			answer(401, "")
		case r.URL.Path == "/queue" && app == "empty":
			answer(200, `{"app":"empty/production","waiting":[],"taken":[]}`)
		case r.URL.Path == "/queue":
			answer(200, `{"app":"shop/production","waiting":[`+queueItem+`],"taken":[`+
				strings.Replace(strings.Replace(queueItem, `"number":482`, `"number":9`, 1), `"taken_at":null`, `"taken_at":"2026-10-16T09:00:00Z"`, 1)+`]}`)
		case r.URL.Path == "/queue/next" && r.Method == http.MethodPost:
			calls++
			switch {
			case app == "down", app == "flaky" && calls == 2:
				answer(500, `{"message":"the database is restarting"}`)
			case app == "empty", (app == "later" || app == "flaky") && calls < 3:
				answer(200, empty)
			default:
				answer(200, taken)
			}
		case r.URL.Path == "/queue/482" && r.Method == http.MethodPut && strings.Contains(string(body), `"ending":"fix"`):
			answer(200, `{"app":"shop/production","pull":482,"ending":"fix","note":"The job ran a query for each table. It now reads the rows once.","fix":"#490","state":"partly"}`)
		case r.URL.Path == "/queue/482" && strings.Contains(string(body), `"ending":"excuse"`):
			answer(200, `{"app":"shop/production","pull":482,"ending":"excuse","note":"The target assumed a warm cache.","state":"confirmed","excused":"chk_1"}`)
		case r.URL.Path == "/queue/482" && strings.Contains(string(body), `"ending":"replace"`):
			answer(200, `{"app":"shop/production","pull":482,"ending":"replace","note":"A timing limit did not test it.","state":"pending","excused":"chk_1","added":"chk_2","warning":"it already passes on the 1d before now"}`)
		case r.URL.Path == "/queue/482":
			answer(422, `{"type":"error","message":"#482 already ended as fix."}`)
		default:
			answer(404, "")
		}
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func TestQueue(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		stdin   string
		logged  string // what the log holds before the command
		code    int
		stdout  string
		stderr  string
		request string
		log     string // what the log holds after, in full
		slept   int
		none    bool
	}{
		{name: "list shows what waits and what a loop holds", args: []string{"queue", "list"}, code: 0,
			stdout:  "=== shop/production queue\n\nPR    VERDICT         STATUS                      TITLE\n#482  worked in part  waiting                     Cache the cart total\n#9    worked in part  taken 2026-10-16 09:00 UTC  Cache the cart total\n",
			request: "GET /queue?repo=acme%2Fshop"},
		{name: "list of an empty queue", args: []string{"queue", "list", "--app", "empty"}, code: 0, stdout: "No failed pull request waits for a loop.\n", request: "GET /queue?app=empty"},
		{name: "list as JSON", args: []string{"queue", "list", "--json"}, code: 0, stdout: `"expected": "The cart loads in under 300 ms."`},
		{name: "next prints the pull request and logs it", args: []string{"queue", "next"}, code: 0,
			stdout:  "=== #482 Cache the cart total\nshop/production\n\nDeploy    9f2c41e8, live 2026-10-14 16:02 UTC\nVerdict   worked in part\n\nFailed    chk_1  The cart is faster\n\nClaim\n  The p95 of GET /cart is under 300 ms.\n\nExpected\n  The cart loads in under 300 ms.\n\nInstead\n  It took 410 ms.\n\nOutcome\n  seen 900, p95_ms 410\n\nIssues\n  SHOP-41  opened, unresolved\n\nReport how it ends with packrat queue done 482.",
			request: "POST /queue/next?repo=acme%2Fshop", log: "2026-10-16 09:30  took   #482  Cache the cart total\n"},
		{name: "next as JSON is the API's answer", args: []string{"queue", "next", "--json"}, code: 0, stdout: `"waiting": 1`, log: "2026-10-16 09:30  took   #482  Cache the cart total\n"},
		{name: "next with nothing waiting says so", args: []string{"queue", "next", "--app", "empty"}, code: 0, stdout: "No failed pull request waits in empty/production.\n"},
		{name: "next with nothing waiting, as JSON", args: []string{"queue", "next", "--app", "empty", "--json"}, code: 0, stdout: `"item": null`},
		{name: "next --wait asks each minute until one waits", args: []string{"queue", "next", "--wait", "--app", "later"}, code: 0, stdout: "=== #482 Cache the cart total",
			stderr: "No failed pull request waits in later/production. Asking again each minute until one does.\n", slept: 2, log: "2026-10-16 09:30  took   #482  Cache the cart total\n"},
		{name: "next --wait asks again after a failure", args: []string{"queue", "next", "--wait", "--app", "flaky"}, code: 0, stdout: "=== #482",
			stderr: "the database is restarting. Asking again in a minute.\n", slept: 2, log: "2026-10-16 09:30  took   #482  Cache the cart total\n"},
		{name: "next --wait gives up when the first answer fails", args: []string{"queue", "next", "--wait", "--app", "down"}, code: 1, stderr: "packrat queue next: ", slept: 0},
		{name: "done with a fix names it and logs the note", args: []string{"queue", "done", "482", "--ending", "fix", "--fix", "#490", "--note", "The job ran a query for each table.\nIt now reads the rows once."},
			logged: "2026-10-16 09:00  took   #482  Cache the cart total\n", code: 0, stdout: "Ended #482 in shop/production: fix.\n#490.\n",
			request: `PUT /queue/482?repo=acme%2Fshop {"ending":"fix","note":"The job ran a query for each table.\nIt now reads the rows once.","fix":"#490"}`,
			log:     "2026-10-16 09:00  took   #482  Cache the cart total\n2026-10-16 09:30  ended  #482  fix · #490\n                         The job ran a query for each table. It now reads the rows once.\n"},
		{name: "done excusing the check says what changed", args: []string{"queue", "done", "https://github.com/acme/shop/pull/482", "--ending", "excuse", "--check", "chk_1", "--note", "The target assumed a warm cache."},
			code: 0, stdout: "Ended #482 in shop/production: excuse the check.\nExcused chk_1 · now worked.\n",
			request: `PUT /queue/482?repo=acme%2Fshop {"ending":"excuse","note":"The target assumed a warm cache.","check":"chk_1"}`,
			log:     "2026-10-16 09:30  ended  #482  excuse the check · excused chk_1 · now worked\n                         The target assumed a warm cache.\n"},
		{name: "done replacing the check sends the new one", args: []string{"queue", "done", "482", "--app", "shop", "--ending", "replace", "--file", "-", "--note", "A timing limit did not test it."},
			stdin: `{"title":"The sync finishes"}`, code: 0, stdout: "Excused chk_1 · added chk_2 · now pending.\n", stderr: "warning: it already passes on the 1d before now",
			request: `PUT /queue/482?app=shop&repo=acme%2Fshop {"ending":"replace","note":"A timing limit did not test it.","new_check":{"title":"The sync finishes"}}`,
			log:     "2026-10-16 09:30  ended  #482  replace the check · excused chk_1 · added chk_2 · now pending\n                         A timing limit did not test it.\n"},
		{name: "done as JSON is the API's answer", args: []string{"queue", "done", "482", "--ending", "excuse", "--note", "The target assumed a warm cache.", "--json"}, code: 0, stdout: `"excused": "chk_1"`,
			log: "2026-10-16 09:30  ended  #482  excuse the check · excused chk_1 · now worked\n                         The target assumed a warm cache.\n"},
		{name: "done with fixed-already sends the ending with an underscore", args: []string{"queue", "done", "482", "--ending", "fixed-already", "--fix", "e50e12be", "--note", "x"}, code: 1,
			stderr: "#482 already ended as fix.", request: `{"ending":"fixed_already","note":"x","fix":"e50e12be"}`},
		{name: "done that Packrat refuses changes no log", args: []string{"queue", "done", "482", "--ending", "needs-person", "--note", "A product decision."}, code: 1,
			stderr: "answered 422: #482 already ended as fix.", request: `{"ending":"needs_person","note":"A product decision."}`},
		{name: "done needs a known ending", args: []string{"queue", "done", "482", "--ending", "merged", "--note", "x"}, code: 2, stderr: "--ending is one of fix, fixed-already, excuse, replace, needs-person", none: true},
		{name: "done needs a note", args: []string{"queue", "done", "482", "--ending", "fix", "--fix", "#490"}, code: 2, stderr: "--note says the cause", none: true},
		{name: "done needs one check as JSON", args: []string{"queue", "done", "482", "--ending", "replace", "--file", "-", "--note", "x"}, stdin: "[]", code: 2, stderr: "holds one check as JSON", none: true},
		{name: "done needs a pull request", args: []string{"queue", "done", "main", "--ending", "fix", "--note", "x"}, code: 2, stderr: "is not a pull request number or URL", none: true},
		{name: "log prints the end of the file", args: []string{"queue", "log", "-n", "2"}, logged: "one\ntwo\nthree\n", code: 0, stdout: "two\nthree\n", none: true, log: "one\ntwo\nthree\n"},
		{name: "log with nothing logged says where it is kept", args: []string{"queue", "log"}, code: 0, stdout: "A loop has logged nothing in this repository. The log is ", none: true},
		{name: "queue needs a command", args: []string{"queue"}, code: 2, stderr: "specify a command: list, next, done or log", none: true},
		{name: "queue takes no other command", args: []string{"queue", "skip", "482"}, code: 2, stderr: "specify a command: list, next, done or log", none: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asked := fakeQueue(t)
			env := map[string]string{"PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
			store := auth.Store{Dir: t.TempDir()}
			if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
				t.Fatalf("saving the sign-in: %v", err)
			}
			common := t.TempDir()
			logPath := filepath.Join(common, "packrat", "loop.log")
			if tc.logged != "" {
				if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(logPath, []byte(tc.logged), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			slept := 0
			w := world{getenv: func(name string) string { return env[name] }, store: store, stdin: strings.NewReader(tc.stdin), http: &http.Client{}, slow: &http.Client{},
				sleep: func(d time.Duration) {
					if d != time.Minute {
						t.Errorf("slept %s, want a minute", d)
					}
					slept++
				},
				hostname: func() (string, error) { return "laptop", nil }, origin: func() string { return "git@github.com:acme/shop.git" },
				now:     func() time.Time { return time.Date(2026, 10, 16, 9, 30, 0, 0, time.Local) },
				program: func(string, ...string) (string, error) { return common + "\n", nil }}
			var stdout, stderr bytes.Buffer

			code := runIn(w, tc.args, &stdout, &stderr)

			if code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout lacks %q:\n%s", tc.stdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), loginKey) {
				t.Errorf("a key was printed:\n%s%s", stdout.String(), stderr.String())
			}
			all := strings.Join(*asked, "\n")
			if tc.request != "" && !strings.Contains(all, tc.request) {
				t.Errorf("no request %q in:\n%s", tc.request, all)
			}
			if tc.none && len(*asked) > 0 {
				t.Errorf("asked Packrat when it must not:\n%s", all)
			}
			if slept != tc.slept {
				t.Errorf("slept %d times, want %d", slept, tc.slept)
			}
			logged, err := os.ReadFile(logPath) // #nosec G304 -- the test's own file
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			want := tc.log
			if want == "" {
				want = tc.logged
			}
			if string(logged) != want {
				t.Errorf("the log holds:\n%s\nwant:\n%s", logged, want)
			}
		})
	}
}

// Outside a git checkout there is nowhere to keep the log: the ending still reaches Packrat, and the command says the
// log was not written.
func TestQueueOutsideAGitCheckout(t *testing.T) {
	server, asked := fakeQueue(t)
	env := map[string]string{"PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
	store := auth.Store{Dir: t.TempDir()}
	if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
		t.Fatalf("saving the sign-in: %v", err)
	}
	w := world{getenv: func(name string) string { return env[name] }, store: store, stdin: strings.NewReader(""), http: &http.Client{}, slow: &http.Client{},
		sleep: func(time.Duration) {}, origin: func() string { return "" }}
	var stdout, stderr bytes.Buffer

	code := runIn(w, []string{"queue", "done", "482", "--app", "shop", "--ending", "excuse", "--note", "The target assumed a warm cache."}, &stdout, &stderr)

	if code != exitOK || !strings.Contains(stdout.String(), "Excused chk_1") {
		t.Errorf("exit %d, stdout %q", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "could not write the loop's log: run it in a git checkout") {
		t.Errorf("stderr lacks the note about the log:\n%s", stderr.String())
	}
	if len(*asked) != 1 {
		t.Errorf("asked %d times, want 1", len(*asked))
	}
}
