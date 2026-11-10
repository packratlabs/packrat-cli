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
			answer(200, `{"access_token":"`+loginKey+`","key":"key_1","email":"dana@example.com"}`)
		case !signedIn:
			answer(401, "")
		case r.URL.Path == "/cli/token":
			answer(204, "")
		case r.URL.Path == "/cli/whoami":
			answer(200, `{"key":"key_1","name":"CLI: laptop","source":"cli","email":"dana@example.com","orgs":[{"org":"org_a","name":"Acme"},{"org":"org_b","name":"Bolt"}]}`)
		case r.URL.Path == "/apps":
			answer(200, `{"now":"2026-10-20T21:00:00Z","apps":[{"app":"shop/production","service":"shop","env":"production","org":"org_a","org_name":"Acme","repository":"acme/shop","first_seen_at":"2026-10-01T00:00:00Z"},{"app":"blog/production","service":"blog","env":"production","org":"org_b","org_name":"Bolt","repository":"bolt/blog","first_seen_at":"2026-10-01T00:00:00Z"}]}`)
		case r.URL.Path == "/issues/SHOP-3/resolve" && r.Method == http.MethodPatch:
			answer(200, `{"id":"SHOP-3","kind":"detected","status":"resolved","resolved_by":"member","reason":"The check measured the wrong thing."}`)
		case r.URL.Path == "/issues/SHOP-3/archive" && r.URL.Query().Get("until") == "forever":
			answer(200, `{"id":"SHOP-3","kind":"detected","status":"archived","archived_until":"forever"}`)
		case r.URL.Path == "/issues/SHOP-3/archive" && r.Method == http.MethodPatch:
			answer(200, `{"id":"SHOP-3","kind":"detected","status":"archived","archived_until":"escalating"}`)
		case r.URL.Path == "/issues/SHOP-3/resolve" && r.Method == http.MethodDelete:
			answer(200, `{"id":"SHOP-3","kind":"detected","status":"unresolved"}`)
		case r.URL.Path == "/issues/SHOP-9/resolve":
			answer(404, `{"message":"shop has no issue SHOP-9"}`)
		case r.URL.Path == "/pulls/482/archive" && r.Method == http.MethodPatch:
			answer(200, `{"app":"shop/production","pull":482,"state":"archived","archive_reason":"Reverted in #483.","deploys":[],"checks":[]}`)
		case r.URL.Path == "/pulls/482/archive" && r.Method == http.MethodDelete:
			answer(200, `{"app":"shop/production","pull":482,"state":"pending","deploys":[],"checks":[]}`)
		case r.URL.Path == "/pulls/490/archive":
			answer(409, `{"message":"#490 is confirmed, and only a pending verification can be archived"}`)
		case r.URL.Path == "/pulls/482":
			answer(200, `{"app":"shop/production","pull":482,"state":"partly","analysed":true,"repository":"acme/shop","deploys":[{"id":"dep_1","version":"9f2c41e8aa","deployed_at":"2026-10-14T16:02:00Z","pull":482,"title":"Cache the cart total","verdict":"partly","intent":"A faster cart","evidence":"p95 840 to 210 ms","next_step":"Key the cache on gift cards","report":"rep_1"}],"checks":[{"id":"chk_1","what":"Gift card totals are right.\nselect 1","due_at":"2026-10-20T21:00:00Z","runs_at":"2026-10-20T21:07:00Z","pull":482,"asked":true,"title":"Gift card totals","source":"asked","result":"failed","runs":[{"at":"2026-10-20T21:07:00Z","result":"failed","evidence":"3 carts off by the card"}]}]}`)
		case r.URL.Path == "/pulls/checks" && r.URL.Query().Get("repo") == "acme/blog":
			answer(400, `{"type":"error","message":"no app is connected to acme/blog on GitHub.","watched":false}`)
		case r.URL.Path == "/pulls/checks":
			answer(200, `{"app":"shop/production","pull":null,"dry_run":true,"checks":[{"title":"The cart is faster","what":"x","after":"1h"}]}`)
		case r.URL.Path == "/pulls/482/checks" && strings.Contains(string(body), "none"):
			answer(200, `{"app":"shop/production","pull":482,"dry_run":false,"none":"Docs only.","checks":[]}`)
		case r.URL.Path == "/pulls/482/checks":
			answer(200, `{"app":"shop/production","pull":482,"dry_run":false,"checks":[{"title":"The cart is faster","what":"x","after":"1h"},{"title":"No checkout errors","what":"y","after":"1d"}]}`)
		case r.URL.Path == "/pulls/9/checks":
			answer(409, `{"type":"error","message":"#9 shipped in 9f2c41e8 at 2026-10-14 16:02 UTC, and its checks are scheduled."}`)
		case r.URL.Path == "/overview":
			answer(200, `{"app":"shop/production","org":"org_a","report":{"id":"rep_1","subject":"Watch (afternoon): deploys recorded early","at":"2026-11-10T19:00:00Z","from":"2026-11-10T13:00:00Z","to":"2026-11-10T19:00:00Z"},`+
				`"items":[{"status":"watch","title":"Migration pod records deploys early","changed":"Migrations logged their versions before any server ran them.","matters":"Deploy times are early.","evidence":"30s early, twice.","next":"Have ingest ignore the migration pod.","issue":"SHOP-70","pull":null,`+
				`"chart":{"kind":"bars","unit":"s","caption":"seconds early","rows":[["2026-11-10 09:00",31,true]]}},`+
				`{"status":"shipped","title":"Cart total cached","changed":"The cart total is cached.","matters":"","evidence":"","next":"Nothing.","issue":null,"pull":{"number":482,"title":"Cache the cart total","state":"confirmed","label":"Worked","url":null},"chart":null}]}`)
		case r.URL.Path == "/issues":
			answer(200, `{"app":"shop","org":"org_a","period":"14d","issues":[{"id":"SHOP-3","kind":"detected","title":"Checkout fails for gift cards","severity":"high","summary":"Gift card carts fail to pay.","status":"unresolved","events":12,"users":4,"normal":"Gift card failures run at this level most days.","errors":["SHOP-1"],"reasons":["rising"]},{"id":"SHOP-2","kind":"error","title":"NoMethodError: undefined method 'total' for nil","severity":null,"status":"unresolved","events":3,"users":1,"part_of":null}]}`)
		case r.URL.Path == "/pulls/7":
			answer(200, `{"app":"shop/production","pull":7,"analysed":false,"repository":"","deploys":[],"checks":[]}`)
		case r.URL.Path == "/checks" && r.URL.Query().Get("app") == "" && r.Method == http.MethodGet:
			answer(400, `{"type":"error","message":"No app specified. This key has access to claude-code and colorbook. Add app=<service>.","apps":["claude-code","colorbook"]}`)
		case r.URL.Path == "/checks" && r.Method == http.MethodPost:
			answer(201, `{"id":"chk_2","what":"x","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}`)
		case r.URL.Path == "/checks":
			answer(200, `{"app":"shop/production","checks":[`+
				`{"id":"chk_9","what":"PR #541 (merge 93428404) makes the check skip a moved item.","due_at":"2026-10-12T20:00:00Z","closed_at":"2026-10-12T20:43:00Z","outcome":"Held: 10 lines."},`+
				`{"id":"chk_1","what":"Gift card totals are right.","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}]}`)
		case r.URL.Path == "/checks/chk_0":
			answer(404, `{"type":"error","message":"No check chk_0 in your org."}`)
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
		org     string
		code    int
		stdout  string
		stderr  string
		request string
		none    bool
	}{
		{name: "login waits for the browser and keeps the key", args: []string{"login", "--no-browser"}, code: 0, stdout: "Signed in as dana@example.com, in every org you belong to.", request: "POST /cli/token device_code=dev"},
		{name: "login with a browser says what to check", args: []string{"login"}, code: 0, stdout: "Confirm that it shows the code BCDF-GHJK"},
		{name: "auth status names the person and the orgs", args: []string{"auth", "status"}, login: true, code: 0, stdout: "Signed in as dana@example.com.\nKey key_1, \"CLI: laptop\", from packrat login.\n\nOrgs\n  Acme  org_a\n  Bolt  org_b\n\nCommands here find the org"},
		{name: "auth status marks the org of this checkout", args: []string{"auth", "status"}, login: true, origin: "git@github.com:bolt/blog.git", code: 0, stdout: "  Acme  org_a\n* Bolt  org_b\n\n* is the org commands read here: an app in it reads bolt/blog, this checkout's origin.", request: "GET /apps?repo=bolt%2Fblog"},
		{name: "auth status marks the org of --org", args: []string{"auth", "status", "--org", "org_a"}, login: true, code: 0, stdout: "* Acme  org_a\n  Bolt  org_b\n\n* is the org commands read here: --org."},
		{name: "--org goes on every request", args: []string{"--org=org_b", "sql", "--app", "shop", "select 1"}, login: true, origin: "git@github.com:acme/shop.git", code: 0, request: "POST /sql?app=shop&org=org_b&repo=acme%2Fshop select 1"},
		{name: "PACKRAT_ORG names the org", args: []string{"check", "list", "--app", "shop"}, org: "org_b", login: true, code: 0, request: "GET /checks?app=shop&org=org_b"},
		{name: "--org needs a value", args: []string{"sql", "--org"}, login: true, code: 2, stderr: "--org needs an org's public id", none: true},
		{name: "auth status says when PACKRAT_API_KEY is used", args: []string{"auth", "status"}, key: statusKey, login: true, code: 0, stdout: "from PACKRAT_API_KEY"},
		{name: "logout revokes the key", args: []string{"logout"}, login: true, code: 0, stdout: "Signed out dana@example.com.", request: "DELETE /cli/token"},
		{name: "logout with no sign-in", args: []string{"logout"}, code: 0, stdout: "You're not signed in."},
		{name: "no key", args: []string{"pr", "show", "482"}, code: 2, stderr: "Run packrat login", none: true},
		{name: "a pull request by URL asks by its repository", args: []string{"pr", "show", "https://github.com/acme/shop/pull/482"}, login: true, code: 0, stdout: "=== #482 Cache the cart total\nshop/production\n\nDeploy    9f2c41e8, live 2026-10-14 16:02 UTC\nVerdict   worked in part\n", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "a pull request by number asks by the origin remote", args: []string{"pr", "show", "#482"}, login: true, origin: "git@github.com:acme/shop.git\n", code: 0, stdout: "Report    packrat open http", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "--app wins over the repository", args: []string{"pr", "show", "482", "--app", "shop"}, login: true, origin: "git@github.com:acme/shop.git", code: 0, stdout: "due 20 Oct 21:00     Gift card totals are right.", request: "GET /pulls/482?app=shop"},
		{name: "a pull request as JSON", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"verdict": "partly"`},
		{name: "a pull request as JSON keeps its state", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"state": "partly"`},
		{name: "a pull request as JSON keeps when each check runs", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"runs_at": "2026-10-20T21:07:00Z"`},
		{name: "a pull request as JSON keeps each check's runs", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"evidence": "3 carts off by the card"`},
		{name: "a pull request with no deploy and no repository", args: []string{"pr", "show", "7"}, login: true, code: 0, stdout: "GitHub repository is not connected"},
		{name: "not a pull request", args: []string{"pr", "show", "main"}, login: true, code: 2, stderr: "is not a pull request number or URL", none: true},
		{name: "a dry run checks a draft by the origin remote", args: []string{"pr", "checks", "set", "--dry-run", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git\n", stdin: `[{"title":"The cart is faster"}]`, code: 0, stdout: "The draft holds 1 check for shop/production.\n\nAFTER  TITLE\n1h     The cart is faster\n", request: `PUT /pulls/checks?dry_run=1&repo=acme%2Fshop [{"title":"The cart is faster"}]`},
		{name: "a pull request declares its checks", args: []string{"pr", "checks", "set", "https://github.com/acme/shop/pull/482", "--file", "-"}, login: true, stdin: "[]", code: 0, stdout: "Declared 2 checks for #482 in shop/production.", request: "PUT /pulls/482/checks?repo=acme%2Fshop []"},
		{name: "a pull request declares none", args: []string{"pr", "checks", "set", "482", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git", stdin: `{"none":"Docs only."}`, code: 0, stdout: "#482 declares no checks in shop/production: Docs only."},
		{name: "a repository Packrat does not verify exits 3", args: []string{"pr", "checks", "set", "--dry-run", "--file", "-"}, login: true, origin: "git@github.com:acme/blog.git", stdin: "[]", code: 3, stderr: "no app is connected to acme/blog"},
		{name: "a shipped pull request says how to add a check", args: []string{"pr", "checks", "set", "9", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git", stdin: "[]", code: 1, stderr: "its checks are scheduled"},
		{name: "declaring needs a file", args: []string{"pr", "checks", "set", "482"}, login: true, code: 2, stderr: "specify the pull request and --file", none: true},
		{name: "declaring needs a pull request unless it is a dry run", args: []string{"pr", "checks", "set", "--file", "-"}, login: true, code: 2, stderr: "specify the pull request and --file", none: true},
		{name: "issues by the origin remote, by priority", args: []string{"issues"}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "=== shop issues · unresolved, events over 14d\n\nID      SEVERITY  EVENTS  USERS  TITLE\nSHOP-3  high          12      4  AI: Checkout fails for gift cards\nSHOP-2  —              3      1  NoMethodError", request: "GET /issues?repo=acme%2Fshop&sort=priority"},
		{name: "overview by the origin remote", args: []string{"overview"}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "=== shop/production · Watch (afternoon): deploys recorded early\nrep_1 · 2026-11-10 19:00 UTC\n\nWATCH  Migration pod records deploys early  SHOP-70\n  Migrations logged their versions before any server ran them.\n  Deploy times are early.\n  30s early, twice.\n  Next: Have ingest ignore the migration pod.\n\nSHIPPED  Cart total cached  #482 Worked\n  The cart total is cached.\n  Next: Nothing.\n\nRead the whole summary with packrat open ", request: "GET /overview?repo=acme%2Fshop"},
		{name: "overview as JSON keeps the chart rows and nulls", args: []string{"overview", "--app", "shop/production", "--json"}, login: true, code: 0, stdout: `"issue": null`, request: "GET /overview?app=shop%2Fproduction"},
		{name: "issues as JSON keep the summary and reasons", args: []string{"issues", "--app", "shop", "--kind", "detected", "--json"}, login: true, code: 0, stdout: `"normal": "Gift card failures run at this level most days."`, request: "GET /issues?app=shop&kind=detected&sort=priority"},
		{name: "issues take only flags or an action", args: []string{"issues", "SHOP-3"}, login: true, code: 2, stderr: "resolve, archive or reopen and an issue ID", none: true},
		{name: "resolve an issue with a reason", args: []string{"issues", "resolve", "SHOP-3", "--reason", "The check measured the wrong thing."}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "Resolved SHOP-3.\nReason: The check measured the wrong thing.\n", request: `PATCH /issues/SHOP-3/resolve?repo=acme%2Fshop {"reason":"The check measured the wrong thing."}`},
		{name: "archive an issue for good", args: []string{"issues", "archive", "SHOP-3", "--until", "forever", "--app", "shop"}, login: true, code: 0, stdout: "Archived SHOP-3 for good.\n", request: "PATCH /issues/SHOP-3/archive?app=shop&until=forever"},
		{name: "archive an issue until it escalates", args: []string{"issues", "archive", "SHOP-3", "--app", "shop"}, login: true, code: 0, stdout: "Archived SHOP-3 until it escalates.\n", request: "PATCH /issues/SHOP-3/archive?app=shop"},
		{name: "reopen an issue", args: []string{"issues", "reopen", "SHOP-3", "--app", "shop"}, login: true, code: 0, stdout: "Reopened SHOP-3.\n", request: "DELETE /issues/SHOP-3/resolve?app=shop"},
		{name: "resolve an issue as JSON", args: []string{"issues", "resolve", "SHOP-3", "--app", "shop", "--json"}, login: true, code: 0, stdout: `"resolved_by": "member"`},
		{name: "until is only for archive", args: []string{"issues", "resolve", "SHOP-3", "--until", "forever"}, login: true, code: 2, stderr: "--until is escalating or forever", none: true},
		{name: "resolve an issue that is not there", args: []string{"issues", "resolve", "SHOP-9", "--app", "shop"}, login: true, code: 1, stderr: "packrat issues resolve: shop has no issue SHOP-9"},
		{name: "archive a pull request's verification", args: []string{"pr", "archive", "482", "--reason", "Reverted in #483."}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "Archived #482 in shop/production and cancelled its open checks.\nReason: Reverted in #483.\n", request: `PATCH /pulls/482/archive?repo=acme%2Fshop {"reason":"Reverted in #483."}`},
		{name: "unarchive a pull request's verification", args: []string{"pr", "unarchive", "https://github.com/acme/shop/pull/482"}, login: true, code: 0,
			stdout: "Moved #482 in shop/production back to pending", request: "DELETE /pulls/482/archive?repo=acme%2Fshop"},
		{name: "a verification with a verdict is not archived", args: []string{"pr", "archive", "490", "--app", "shop"}, login: true, code: 1, stderr: "only a pending verification can be archived"},
		{name: "pr needs show", args: []string{"pr", "482"}, login: true, code: 2, stderr: "specify a command: show", none: true},
		{name: "a check for a pull request asks by the origin remote", args: []string{"check", "add", "--pull", "482", "--due", "2026-10-20T21:00Z"}, login: true, origin: "git@github.com:acme/shop.git\n", stdin: "Gift card totals are right.\nselect 1", code: 0, stdout: `"id": "chk_2"`, request: "POST /checks?due=2026-10-20T21%3A00Z&pull=482&repo=acme%2Fshop Gift card totals are right."},
		{name: "a check needs a time", args: []string{"check", "add", "--pull", "482"}, login: true, stdin: "x", code: 2, stderr: "specify --due, the earliest time", none: true},
		{name: "a check needs a text", args: []string{"check", "add", "--pull", "482", "--due", "2026-10-20T21:00Z"}, login: true, code: 2, stderr: "on stdin", none: true},
		{name: "a check from a report", args: []string{"check", "add", "--app", "shop", "--report", "rep_1", "--due", "2026-10-20T21:00Z"}, login: true, stdin: "It held.", code: 0, stdout: `"id": "chk_2"`, request: "POST /checks?app=shop&due=2026-10-20T21%3A00Z&report=rep_1 It held."},
		{name: "check with no command", args: []string{"check"}, login: true, code: 2, stderr: "Run packrat help check", none: true},
		{name: "check list puts open ones first", args: []string{"check", "list", "--app", "shop"}, login: true, code: 0,
			stdout: "chk_1\n—      closed 12 Oct 20:43  PR #541 (merge 93428404) makes the check skip"},
		{name: "check show reads the check by its id", args: []string{"check", "show", "chk_1"}, login: true, code: 0,
			stdout: "chk_1 · closed 2026-10-16 10:00 UTC\n\nAsked\n  x\n\nResult\n  Closed by a developer before it was verified.\n", request: "GET /checks/chk_1"},
		{name: "check show of one that is not there", args: []string{"check", "show", "chk_0"}, login: true, code: 1, stderr: "packrat check show: No check chk_0 in your org."},
		{name: "check list", args: []string{"check", "list", "--app", "shop", "--pull", "482"}, login: true, code: 0, stdout: "=== shop/production checks\n\nPR     STATUS", request: "GET /checks?app=shop&pull=482"},
		{name: "no app, with more than one", args: []string{"check", "list"}, login: true, code: 2, stderr: "Error: No app specified. This key has access to claude-code and colorbook.\nUSAGE: packrat check list --app <app-name>\n"},
		{name: "check close", args: []string{"check", "close", "chk_1"}, login: true, code: 0, stdout: "Closed by a developer", request: "DELETE /checks/chk_1"},
		{name: "sql prints rows and a summary", args: []string{"sql", "--app", "shop", "select 1"}, login: true, code: 0, stdout: `"values":[1]`, stderr: "1 rows in 0.01s from shop/production", request: "POST /sql?app=shop select 1"},
		{name: "sql from stdin", args: []string{"sql"}, login: true, stdin: "select 2", code: 0, request: "POST /sql select 2"},
		{name: "sql with no script", args: []string{"sql"}, login: true, code: 2, stderr: "the query is empty", none: true},
		{name: "open a pasted curl line reads the report", args: []string{"open", `curl -sS QUERY/reports/rep_1 -H "Authorization: Bearer $(sed -nE 's/^PACKRAT_(API|QUERY)_KEY=//p' .env | tail -1)"`}, login: true, code: 0, stdout: "# Worked in part: #482", request: "GET /reports/rep_1"},
		{name: "open a page", args: []string{"open", "SITE/orgs/org_a/apps/shop/production/analyst"}, login: true, code: 0, stdout: "<h1>Analyst</h1>"},
		{name: "open a pull request", args: []string{"open", "https://github.com/acme/shop/pull/482"}, login: true, code: 0, stdout: "=== #482 Cache the cart total", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "open never sends the key elsewhere", args: []string{"open", "https://example.com/orgs/x"}, login: true, code: 1, stderr: "not a Packrat URL", none: true},
		{name: "open over plain http to packrat.sh is refused", args: []string{"open", "http://packrat.sh/orgs/x"}, login: true, code: 1, stderr: "not a Packrat URL", none: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asked := fakePackrat(t)
			env := map[string]string{"PACKRAT_API_KEY": tc.key, "PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL, "PACKRAT_ORG": tc.org}
			store := auth.Store{Dir: t.TempDir()}
			if tc.login {
				if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
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
				if err != nil || signIn.Token != loginKey || signIn.Email != "dana@example.com" {
					t.Errorf("kept %+v (%v), want dana's key", signIn, err)
				}
				if (browsed != "") == (len(tc.args) > 1) {
					t.Errorf("browsed %q with args %v", browsed, tc.args)
				}
			}
		})
	}
}
