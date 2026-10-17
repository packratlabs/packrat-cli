package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

const loginKey = "packrat_query_fromlogin"

// fakePackrat answers the site's and the API's addresses the way packrat.sh and query.packrat.sh do,
// and keeps each request it was sent.
func fakePackrat(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	polls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		signedIn := r.Header.Get("Authorization") == "Bearer "+statusKey || r.Header.Get("Authorization") == "Bearer "+loginKey
		switch {
		case r.URL.Path == "/cli/sign_ins":
			answer(201, `{"device_code":"dev","user_code":"BCDF-GHJK","verification_uri":"`+server.URL+`/cli/activate","verification_uri_complete":"`+server.URL+`/cli/activate?code=BCDF-GHJK","expires_in":600,"interval":5}`)
		case r.URL.Path == "/cli/token" && r.Method == http.MethodPost:
			polls++
			if polls < 3 {
				answer(400, `{"error":"authorization_pending"}`)
				return
			}
			answer(200, `{"access_token":"`+loginKey+`","key":"key_1","org":"org_a","org_name":"Acme","email":"dana@example.com"}`)
		case !signedIn:
			answer(401, "")
		case r.URL.Path == "/cli/token":
			answer(204, "")
		case r.URL.Path == "/cli/whoami":
			answer(200, `{"key":"key_1","name":"CLI: laptop","source":"cli","org":"org_a","org_name":"Acme","email":"dana@example.com"}`)
		case r.URL.Path == "/pulls/482":
			answer(200, `{"app":"shop/production","pull":482,"analysed":true,"repository":"acme/shop","deploys":[{"id":"dep_1","version":"9f2c41e8aa","deployed_at":"2026-10-14T16:02:00Z","pull":482,"title":"Cache the cart total","verdict":"partly","intent":"A faster cart","evidence":"p95 840 to 210 ms","next_step":"Key the cache on gift cards","report":"rep_1"}],"checks":[{"id":"chk_1","what":"Gift card totals are right.\nselect 1","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}]}`)
		case r.URL.Path == "/pulls/7":
			answer(200, `{"app":"shop/production","pull":7,"analysed":false,"repository":"","deploys":[],"checks":[]}`)
		case r.URL.Path == "/checks" && r.URL.Query().Get("app") == "" && r.Method == http.MethodGet:
			answer(400, `{"type":"error","message":"No app specified. This key opens claude-code and colorbook. Add app=<service>.","apps":["claude-code","colorbook"]}`)
		case r.URL.Path == "/checks" && r.Method == http.MethodPost:
			answer(201, `{"id":"chk_2","what":"x","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}`)
		case r.URL.Path == "/checks":
			answer(200, `{"app":"shop/production","checks":[`+
				`{"id":"chk_9","what":"PR #541 (merge 93428404) makes the check skip a moved item.","due_at":"2026-10-12T20:00:00Z","closed_at":"2026-10-12T20:43:00Z","outcome":"Held: 10 lines."},`+
				`{"id":"chk_1","what":"Gift card totals are right.","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}]}`)
		case r.URL.Path == "/checks/chk_1":
			answer(200, `{"id":"chk_1","what":"x","due_at":"2026-10-20T21:00:00Z","closed_at":"2026-10-16T10:00:00Z","outcome":"Closed by a developer before it was verified."}`)
		case r.URL.Path == "/sql":
			answer(200, "{\"type\":\"schema\"}\n{\"type\":\"row\",\"values\":[1]}\n{\"type\":\"end\",\"rows\":1,\"seconds\":0.01,\"app\":\"shop/production\"}\n")
		case r.URL.Path == "/reports/rep_1":
			answer(200, "# Worked in part: #482\n")
		case strings.HasPrefix(r.URL.Path, "/orgs/"):
			answer(200, "<html><h1>Analyst</h1></html>")
		default:
			answer(404, "")
		}
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func TestCommandsThatAskPackrat(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		key     string
		login   bool
		stdin   string
		origin  string
		code    int
		stdout  string
		stderr  string
		request string
		none    bool
	}{
		{name: "login waits for the browser and keeps the key", args: []string{"login", "--no-browser"}, code: 0, stdout: "Signed in to Acme as dana@example.com.", request: "POST /cli/token device_code=dev"},
		{name: "login with a browser says what to check", args: []string{"login"}, code: 0, stdout: "Check that it shows the code BCDF-GHJK"},
		{name: "auth status names the key of the sign-in", args: []string{"auth", "status"}, login: true, code: 0, stdout: "Signed in to Acme (org_a) as dana@example.com."},
		{name: "auth status says when PACKRAT_API_KEY is used", args: []string{"auth", "status"}, key: statusKey, login: true, code: 0, stdout: "from PACKRAT_API_KEY"},
		{name: "logout revokes the key", args: []string{"logout"}, login: true, code: 0, stdout: "Signed out of Acme.", request: "DELETE /cli/token"},
		{name: "logout with no sign-in", args: []string{"logout"}, code: 0, stdout: "Not signed in."},
		{name: "no key", args: []string{"pr", "482"}, code: 2, stderr: "Run packrat login", none: true},
		{name: "a pull request by URL asks by its repository", args: []string{"pr", "https://github.com/acme/shop/pull/482"}, login: true, code: 0, stdout: "Deploy 9f2c41e8, live 2026-10-14 16:02 UTC: worked in part  Cache the cart total", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "a pull request by number asks by the origin remote", args: []string{"pr", "#482"}, login: true, origin: "git@github.com:acme/shop.git\n", code: 0, stdout: "packrat open http", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "--app wins over the repository", args: []string{"pr", "482", "--app", "shop"}, login: true, origin: "git@github.com:acme/shop.git", code: 0, stdout: "#482   due 20 Oct 21:00     Gift card totals are right.", request: "GET /pulls/482?app=shop"},
		{name: "a pull request as JSON", args: []string{"pr", "482", "--json"}, login: true, code: 0, stdout: `"verdict": "partly"`},
		{name: "a pull request with no deploy and no repository", args: []string{"pr", "7"}, login: true, code: 0, stdout: "GitHub repository is not connected"},
		{name: "not a pull request", args: []string{"pr", "main"}, login: true, code: 2, stderr: "is not a pull request's number or URL", none: true},
		{name: "a check for a pull request", args: []string{"pr", "482", "check", "--due", "2026-10-20T21:00Z", "--app", "shop"}, login: true, stdin: "Gift card totals are right.\nselect 1", code: 0, stdout: `"id": "chk_2"`, request: "POST /checks?app=shop&due=2026-10-20T21%3A00Z&pull=482 Gift card totals are right."},
		{name: "a check needs a time", args: []string{"pr", "482", "check"}, login: true, stdin: "x", code: 2, stderr: "--due is the first time", none: true},
		{name: "a check needs a text", args: []string{"pr", "482", "check", "--due", "2026-10-20T21:00Z"}, login: true, code: 2, stderr: "on stdin", none: true},
		{name: "a check from a report", args: []string{"check", "add", "--app", "shop", "--report", "rep_1", "--due", "2026-10-20T21:00Z"}, login: true, stdin: "It held.", code: 0, stdout: `"id": "chk_2"`, request: "POST /checks?app=shop&due=2026-10-20T21%3A00Z&report=rep_1 It held."},
		{name: "check with no command", args: []string{"check"}, login: true, code: 2, stderr: "Run packrat help check", none: true},
		{name: "check list puts open ones first", args: []string{"check", "list", "--app", "shop"}, login: true, code: 0,
			stdout: "chk_1\n—      closed 12 Oct 20:43  PR #541 (merge 93428404) makes the check skip"},
		{name: "check show prints the whole check", args: []string{"check", "show", "chk_9", "--app", "shop"}, login: true, code: 0,
			stdout: "chk_9 · closed 2026-10-12 20:43 UTC\n\nAsked\n  PR #541 (merge 93428404) makes the check skip a moved item.\n\nResult\n  Held: 10 lines.\n"},
		{name: "check show of one that is not there", args: []string{"check", "show", "chk_0", "--app", "shop"}, login: true, code: 1, stderr: "chk_0 is not open, and did not close this week, on shop/production"},
		{name: "check list", args: []string{"check", "list", "--app", "shop", "--pull", "482"}, login: true, code: 0, stdout: "=== shop/production checks\n\nPR     STATUS", request: "GET /checks?app=shop&pull=482"},
		{name: "no app, with more than one", args: []string{"check", "list"}, login: true, code: 2, stderr: "Error: No app specified. This key opens claude-code and colorbook.\nUSAGE: packrat check list --app <app-name>\n"},
		{name: "check close", args: []string{"check", "close", "chk_1"}, login: true, code: 0, stdout: "Closed by a developer", request: "DELETE /checks/chk_1"},
		{name: "sql prints rows and a summary", args: []string{"sql", "--app", "shop", "select 1"}, login: true, code: 0, stdout: `"values":[1]`, stderr: "1 rows in 0.01s from shop/production", request: "POST /sql?app=shop select 1"},
		{name: "sql from stdin", args: []string{"sql"}, login: true, stdin: "select 2", code: 0, request: "POST /sql select 2"},
		{name: "sql with no script", args: []string{"sql"}, login: true, code: 2, stderr: "the script is empty", none: true},
		{name: "open a pasted curl line reads the report", args: []string{"open", `curl -sS QUERY/reports/rep_1 -H "Authorization: Bearer $(sed -nE 's/^PACKRAT_(API|QUERY)_KEY=//p' .env | tail -1)"`}, login: true, code: 0, stdout: "# Worked in part: #482", request: "GET /reports/rep_1"},
		{name: "open a page", args: []string{"open", "SITE/orgs/org_a/apps/shop/production/analyst"}, login: true, code: 0, stdout: "<h1>Analyst</h1>"},
		{name: "open a pull request", args: []string{"open", "https://github.com/acme/shop/pull/482"}, login: true, code: 0, stdout: "packrat pr  #482", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "open never sends the key elsewhere", args: []string{"open", "https://example.com/orgs/x"}, login: true, code: 1, stderr: "not a Packrat address", none: true},
		{name: "open over plain http to packrat.sh is refused", args: []string{"open", "http://packrat.sh/orgs/x"}, login: true, code: 1, stderr: "not a Packrat address", none: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asked := fakePackrat(t)
			env := map[string]string{"PACKRAT_API_KEY": tc.key, "PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
			store := auth.Store{Dir: t.TempDir()}
			if tc.login {
				if err := store.Save(auth.SignIn{Org: "org_a", OrgName: "Acme", Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
					t.Fatalf("saving the sign-in: %v", err)
				}
			}
			browsed := ""
			w := world{getenv: func(name string) string { return env[name] }, sleep: func(time.Duration) {}, store: store,
				stdin: strings.NewReader(tc.stdin), http: &http.Client{}, hostname: func() (string, error) { return "laptop", nil },
				origin: func() string { return tc.origin }, browse: func(address string) error { browsed = address; return nil }}
			args := make([]string, len(tc.args))
			for i, arg := range tc.args {
				args[i] = strings.NewReplacer("QUERY", server.URL, "SITE", server.URL).Replace(arg)
			}
			var stdout, stderr bytes.Buffer

			code := runIn(w, args, &stdout, &stderr)

			if code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout lacks %q:\n%s", tc.stdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, stderr.String())
			}
			for _, key := range []string{statusKey, loginKey} {
				if strings.Contains(stdout.String()+stderr.String(), key) {
					t.Errorf("a key was printed:\n%s%s", stdout.String(), stderr.String())
				}
			}
			all := strings.Join(*asked, "\n")
			if tc.request != "" && !strings.Contains(all, tc.request) {
				t.Errorf("no request %q in:\n%s", tc.request, all)
			}
			if tc.none && len(*asked) > 0 {
				t.Errorf("asked Packrat when it must not:\n%s", all)
			}
			if tc.args[0] == "login" && tc.code == 0 {
				signIn, err := store.Current()
				if err != nil || signIn.Token != loginKey || signIn.Org != "org_a" {
					t.Errorf("kept %+v (%v), want the key of org_a", signIn, err)
				}
				if (browsed != "") == (len(tc.args) > 1) {
					t.Errorf("browsed %q with args %v", browsed, tc.args)
				}
			}
		})
	}
}
