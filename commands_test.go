package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/auth"
)

const (
	loginKey = "packrat_api_fromlogin"
	madeKey  = "packrat_api_madebykeys"
)

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
		case r.URL.Query().Get("repo") == "acme/blog":
			answer(400, `{"type":"error","message":"no app is connected to acme/blog on GitHub.","watched":false}`)
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
		case r.URL.Path == "/issues/SHOP-2" && r.Method == http.MethodGet:
			answer(200, `{"id":"SHOP-2","kind":"error","title":"NoMethodError: undefined method 'total' for nil","culprit":"CartsController#show","status":"unresolved","substatus":"regressed","severity":null,"events":3,"users":1,"times_seen":3,"first_seen":"2026-10-12T08:00:00Z","last_seen":"2026-10-20T20:41:00Z","part_of":"SHOP-3","first_release":"9f2c41e8aa","last_release":"a71b0c33dd","tags":{"browser":{"top":"Safari","count":2}},"event":{"event_id":"ev_1","occurred_at":"2026-10-20T20:41:00Z","release":"a71b0c33dd","transaction":"GET /cart","trace_id":"4bf92f35","exceptions":[{"type":"NoMethodError","value":"undefined method 'total' for nil","frames":[{"filename":"gems/rack.rb","lineno":9,"function":"call","in_app":false},{"filename":"app/controllers/carts_controller.rb","lineno":12,"function":"show","in_app":true},{"filename":"app/models/cart.rb","lineno":40,"function":"total","in_app":true}]}]}}`)
		case r.URL.Path == "/issues/SHOP-3" && r.Method == http.MethodGet:
			answer(200, `{"id":"SHOP-3","kind":"detected","title":"Checkout fails for gift cards","severity":"high","summary":"Gift card carts fail to pay.","status":"unresolved","events":3,"users":1,"times_seen":0,"first_seen":"2026-10-12T08:00:00Z","last_seen":"2026-10-20T20:41:00Z","errors":["SHOP-2"],"pull":486,"tags":{},"event":null}`)
		case r.URL.Path == "/issues/SHOP-9/resolve" || r.URL.Path == "/issues/SHOP-9":
			answer(404, `{"message":"shop has no issue SHOP-9"}`)
		case r.URL.Path == "/pulls/482/archive" && r.Method == http.MethodPatch:
			answer(200, `{"app":"shop/production","pull":482,"state":"archived","archive_reason":"Reverted in #483.","deploys":[],"checks":[]}`)
		case r.URL.Path == "/pulls/482/archive" && r.Method == http.MethodDelete:
			answer(200, `{"app":"shop/production","pull":482,"state":"pending","deploys":[],"checks":[]}`)
		case r.URL.Path == "/pulls/482/seen" && r.Method == http.MethodPut:
			answer(200, `{"app":"shop/production","pull":482,"state":"pending","unread":false,"deploys":[],"checks":[{"id":"chk_1","what":"x","due_at":"2026-10-20T21:00:00Z","new":false}]}`)
		case r.URL.Path == "/pulls/seen" && r.Method == http.MethodPut:
			answer(200, `{"app":"shop/production","seen":12}`)
		case r.URL.Path == "/issues/SHOP-3/seen" && r.Method == http.MethodPut:
			answer(200, `{"id":"SHOP-3","kind":"detected","status":"unresolved","unread":false}`)
		case r.URL.Path == "/issues/seen" && r.Method == http.MethodPut:
			answer(200, `{"app":"shop","seen":1}`)
		case r.URL.Path == "/pulls/490/archive":
			answer(409, `{"message":"#490 is confirmed, and only a pending verification can be archived"}`)
		case r.URL.Path == "/pulls/483":
			answer(200, `{"app":"shop/production","pull":483,"state":"pending","analysed":true,"deploys":[],"checks":[{"id":"chk_3","what":"Filters by brand.","title":"Filters by brand","due_at":"2026-10-23T16:02:00Z","pull":483,"kind":"appears","seen":40,"min_seen":200}],"dropped":[{"title":"Gift cards","dropped":"Rejected twice."}]}`)
		case r.URL.Path == "/pulls/482":
			answer(200, `{"app":"shop/production","pull":482,"state":"partly","analysed":true,"repository":"acme/shop","deploys":[{"id":"dep_1","version":"9f2c41e8aa","deployed_at":"2026-10-14T16:02:00Z","pull":482,"title":"Cache the cart total","verdict":"partly","intent":"A faster cart","evidence":"p95 840 to 210 ms","next_step":"Key the cache on gift cards","report":"rep_1"}],"checks":[{"id":"chk_1","what":"Gift card totals are right.\nselect 1","due_at":"2026-10-20T21:00:00Z","runs_at":"2026-10-20T21:07:00Z","pull":482,"asked":true,"title":"Gift card totals","source":"asked","result":"failed","runs":[{"at":"2026-10-20T21:07:00Z","result":"failed","evidence":"3 carts off by the card"}]}],"settled":true,"issues":[{"id":"SHOP-41","link":"opened","status":"resolved","fixed_by":486,"check":"chk_1"}]}`)
		case r.URL.Path == "/pulls/checks":
			answer(200, `{"app":"shop/production","pull":null,"dry_run":true,"checks":[{"title":"The cart is faster","claim":"x","window":"1h"}],"warnings":["check 1 (The cart is faster): it already passes on the 1h before now, so passing would prove nothing about the change"]}`)
		case r.URL.Path == "/pulls/482/checks" && strings.Contains(string(body), "none"):
			answer(200, `{"app":"shop/production","pull":482,"dry_run":false,"none":"Docs only.","checks":[]}`)
		case r.URL.Path == "/pulls/482/checks":
			answer(200, `{"app":"shop/production","pull":482,"dry_run":false,"checks":[{"title":"The cart is faster","claim":"x","window":"1h"},{"title":"No checkout errors","claim":"y","window":"1d"}]}`)
		case r.URL.Path == "/pulls/9/checks":
			answer(409, `{"type":"error","message":"#9 shipped in 9f2c41e8 at 2026-10-14 16:02 UTC, and its checks are scheduled."}`)
		case r.URL.Path == "/issues":
			answer(200, `{"app":"shop","org":"org_a","period":"14d","issues":[{"id":"SHOP-3","kind":"detected","title":"Checkout fails for gift cards","severity":"high","summary":"Gift card carts fail to pay.","status":"unresolved","events":12,"users":4,"normal":"Gift card failures run at this level most days.","errors":["SHOP-1"],"reasons":["rising"]},{"id":"SHOP-2","kind":"error","title":"NoMethodError: undefined method 'total' for nil","severity":null,"status":"unresolved","events":3,"users":1,"part_of":null}]}`)
		case r.URL.Path == "/pulls/9":
			answer(200, `{"app":"shop/production","pull":9,"state":"partly","analysed":true,"repository":"acme/shop","deploys":[],"checks":[],"queue":{"taken_at":"2026-10-16T09:00:00Z","ended_at":"2026-10-16T09:04:00Z","ending":"fix","note":"The job ran a query for each table. It now reads the rows once.","fix":"#490"}}`)
		case r.URL.Path == "/pulls/8":
			answer(200, `{"app":"shop/production","pull":8,"state":"not_observable","analysed":true,"repository":"acme/shop","none":"Renames a heading.","deploys":[],"checks":[]}`)
		case r.URL.Path == "/pulls/7":
			answer(200, `{"app":"shop/production","pull":7,"analysed":false,"repository":"","deploys":[],"checks":[]}`)
		case r.URL.Path == "/checks" && r.URL.Query().Get("app") == "" && r.Method == http.MethodGet:
			answer(400, `{"type":"error","message":"No app specified. This key has access to claude-code and colorbook. Add app=<service>.","apps":["claude-code","colorbook"]}`)
		case r.URL.Path == "/checks" && r.Method == http.MethodPost:
			answer(201, `{"id":"chk_2","what":"x","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true,"warning":"it already passes on the 1h before now"}`)
		case r.URL.Path == "/checks":
			answer(200, `{"app":"shop/production","checks":[`+
				`{"id":"chk_9","what":"PR #541 (merge 93428404) makes the check skip a moved item.","due_at":"2026-10-12T20:00:00Z","closed_at":"2026-10-12T20:43:00Z","outcome":"Held: 10 lines."},`+
				`{"id":"chk_1","what":"Gift card totals are right.","due_at":"2026-10-20T21:00:00Z","pull":482,"asked":true}]}`)
		case r.URL.Path == "/checks/chk_2":
			answer(200, `{"id":"chk_2","what":"y","due_at":"2026-10-20T21:00:00Z","closed_at":"2026-10-16T10:05:00Z","result":"passed","outcome":"seen 199, matched 4","kind":"appears"}`)
		case r.URL.Path == "/checks/chk_0":
			answer(404, `{"type":"error","message":"No check chk_0 in your org."}`)
		case r.URL.Path == "/checks/chk_1":
			answer(200, `{"id":"chk_1","what":"x","due_at":"2026-10-20T21:00:00Z","closed_at":"2026-10-16T10:00:00Z","outcome":"Closed by a developer before it was verified."}`)
		case r.URL.Path == "/feedback" && strings.Contains(string(body), "pull request"):
			answer(429, `{"type":"error","message":"too many reports from this key. Try again in an hour."}`)
		case r.URL.Path == "/keys" && r.Header.Get("Authorization") == "Bearer "+statusKey:
			answer(403, `{"type":"error","message":"an org's key cannot make keys. Run packrat login and make it as yourself."}`)
		case r.URL.Path == "/keys":
			answer(201, `{"id":"key_9","name":"deploy agent","org":"org_a","org_name":"Acme","token":"`+madeKey+`"}`)
		case r.URL.Path == "/feedback":
			answer(202, `{"id":"fbk_5k2m9x0qv7ha","message":"Sent to the Packrat team as fbk_5k2m9x0qv7ha."}`)
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
		{name: "login with a browser says what to check", args: []string{"login"}, code: 0, stdout: "Confirm that the page shows the code BCDF-GHJK"},
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
		{name: "a rule check shows how far it has got", args: []string{"pr", "show", "483", "--app", "shop"}, login: true, code: 0, stdout: "seen 40/200          Filters by brand", request: "GET /pulls/483?app=shop"},
		{name: "a pull request as JSON", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"verdict": "partly"`},
		{name: "a pull request as JSON keeps its state", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"state": "partly"`},
		{name: "a pull request as JSON keeps when each check runs", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"runs_at": "2026-10-20T21:07:00Z"`},
		{name: "a pull request as JSON keeps the issues it opened and who fixed them", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"fixed_by": 486`},
		{name: "a pull request as JSON keeps whether it is settled", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"settled": true`},
		{name: "a pull request as JSON keeps each check's runs", args: []string{"pr", "show", "482", "--json"}, login: true, code: 0, stdout: `"evidence": "3 carts off by the card"`},
		{name: "a pull request a loop ended says how", args: []string{"pr", "show", "9"}, login: true, code: 0,
			stdout: "=== Loop\nA loop ended it 2026-10-16 09:04 UTC: fix #490. The job ran a query for each table."},
		{name: "a pull request as JSON keeps what a loop did", args: []string{"pr", "show", "9", "--json"}, login: true, code: 0, stdout: `"ending": "fix"`},
		{name: "a pull request that declared no checks says why", args: []string{"pr", "show", "8"}, login: true, code: 0, stdout: "=== Checks\nNo checks: Renames a heading.\n"},
		{name: "a pull request as JSON keeps why it declared no checks", args: []string{"pr", "show", "8", "--json"}, login: true, code: 0, stdout: `"none": "Renames a heading."`},
		{name: "a pull request with no deploy and no repository", args: []string{"pr", "show", "7"}, login: true, code: 0, stdout: "GitHub repository is not connected"},
		{name: "not a pull request", args: []string{"pr", "show", "main"}, login: true, code: 2, stderr: "is not a pull request number or URL", none: true},
		{name: "a dry run checks a draft by the origin remote", args: []string{"pr", "checks", "set", "--dry-run", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git\n", stdin: `[{"title":"The cart is faster"}]`, code: 0, stdout: "The draft holds 1 check for shop/production.\n\nWINDOW TITLE\n1h     The cart is faster\n\nWarning: check 1 (The cart is faster): it already passes on the 1h before now, so passing would prove nothing about the change\n", request: `PUT /pulls/checks?dry_run=1&repo=acme%2Fshop [{"title":"The cart is faster"}]`},
		{name: "a pull request declares its checks", args: []string{"pr", "checks", "set", "https://github.com/acme/shop/pull/482", "--file", "-"}, login: true, stdin: "[]", code: 0, stdout: "Declared 2 checks for #482 in shop/production.", request: "PUT /pulls/482/checks?repo=acme%2Fshop []"},
		{name: "a pull request declares none", args: []string{"pr", "checks", "set", "482", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git", stdin: `{"none":"Docs only."}`, code: 0, stdout: "#482 declares no checks in shop/production: Docs only."},
		{name: "a repository Packrat does not verify exits 3", args: []string{"pr", "checks", "set", "--dry-run", "--file", "-"}, login: true, origin: "git@github.com:acme/blog.git", stdin: "[]", code: 3, stderr: "no app is connected to acme/blog"},
		{name: "pr show in a repository Packrat does not verify exits 3", args: []string{"pr", "show", "#482"}, login: true, origin: "git@github.com:acme/blog.git", code: 3, stderr: "packrat pr show: no app is connected to acme/blog"},
		{name: "issues in a repository Packrat does not verify exits 3", args: []string{"issues", "--json"}, login: true, origin: "git@github.com:acme/blog.git", code: 3, stderr: "packrat issues: no app is connected to acme/blog"},
		{name: "a shipped pull request says how to add a check", args: []string{"pr", "checks", "set", "9", "--file", "-"}, login: true, origin: "git@github.com:acme/shop.git", stdin: "[]", code: 1, stderr: "its checks are scheduled"},
		{name: "declaring needs a file", args: []string{"pr", "checks", "set", "482"}, login: true, code: 2, stderr: "specify the pull request and --file", none: true},
		{name: "declaring needs a pull request unless it is a dry run", args: []string{"pr", "checks", "set", "--file", "-"}, login: true, code: 2, stderr: "specify the pull request and --file", none: true},
		{name: "issues by the origin remote, by priority", args: []string{"issues"}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "=== shop issues · unresolved, events over 14d\n\nID      SEVERITY  EVENTS  USERS  TITLE\nSHOP-3  high          12      4  AI: Checkout fails for gift cards\nSHOP-2  —              3      1  NoMethodError", request: "GET /issues?repo=acme%2Fshop&sort=priority"},
		{name: "issues as JSON keep the summary and reasons", args: []string{"issues", "--app", "shop", "--kind", "detected", "--json"}, login: true, code: 0, stdout: `"normal": "Gift card failures run at this level most days."`, request: "GET /issues?app=shop&kind=detected&sort=priority"},
		{name: "issues take only flags or an action", args: []string{"issues", "SHOP-3"}, login: true, code: 2, stderr: "resolve, archive, reopen or seen and an issue ID", none: true},
		{name: "show an error with its latest event, innermost in-app frame first", args: []string{"issues", "show", "SHOP-2"}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout:  "=== SHOP-2 NoMethodError: undefined method 'total' for nil\nStatus      unresolved, regressed\nSeverity    unrated\nEvents      3 from 1 user\nFirst seen  2026-10-12 08:00 UTC in 9f2c41e8\nLast seen   2026-10-20 20:41 UTC in a71b0c33\nWhere       CartsController#show\nPart of     SHOP-3\n\n=== Latest event ev_1\n2026-10-20 20:41 UTC · a71b0c33 · GET /cart\nTrace 4bf92f35\n\nNoMethodError: undefined method 'total' for nil\n  app/models/cart.rb:40 in total\n  app/controllers/carts_controller.rb:12 in show\n\n=== Tags (commonest value)\nbrowser              Safari (2)\n",
			request: "GET /issues/SHOP-2?repo=acme%2Fshop"},
		{name: "show an AI-detected issue with the errors under it", args: []string{"issues", "show", "SHOP-3", "--app", "shop"}, login: true, code: 0,
			stdout: "=== SHOP-3 AI: Checkout fails for gift cards\nStatus      unresolved\nSeverity    high\nFirst seen  2026-10-12 08:00 UTC\nLast seen   2026-10-20 20:41 UTC\nErrors      SHOP-2\nPull        #486 (packrat pr show 486)\n\nSummary\n  Gift card carts fail to pay.\n", request: "GET /issues/SHOP-3?app=shop"},
		{name: "show an issue as JSON keeps the event", args: []string{"issues", "show", "SHOP-2", "--app", "shop", "--json"}, login: true, code: 0, stdout: `"transaction": "GET /cart"`},
		{name: "show an issue that is not there", args: []string{"issues", "show", "SHOP-9", "--app", "shop"}, login: true, code: 1, stderr: "packrat issues show: shop has no issue SHOP-9"},
		{name: "show needs one issue ID", args: []string{"issues", "show"}, login: true, code: 2, stderr: "show, resolve, archive, reopen or seen and an issue ID", none: true},
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
		{name: "mark a pull request seen", args: []string{"pr", "seen", "482"}, login: true, origin: "git@github.com:acme/shop.git", code: 0,
			stdout: "Marked #482 in shop/production seen.\n", request: "PUT /pulls/482/seen?repo=acme%2Fshop"},
		{name: "a pull request marked seen as JSON keeps unread and new", args: []string{"pr", "seen", "482", "--app", "shop", "--json"}, login: true, code: 0, stdout: `"new": false`},
		{name: "mark every pull request seen", args: []string{"pr", "seen", "--all", "--app", "shop"}, login: true, code: 0,
			stdout: "Marked 12 pull requests in shop/production seen.\n", request: "PUT /pulls/seen?app=shop"},
		{name: "mark an issue seen", args: []string{"issues", "seen", "SHOP-3", "--app", "shop"}, login: true, code: 0, stdout: "Marked SHOP-3 seen.\n", request: "PUT /issues/SHOP-3/seen?app=shop"},
		{name: "mark every issue seen", args: []string{"issues", "seen", "--all", "--app", "shop"}, login: true, code: 0, stdout: "Marked 1 issue in shop seen.\n", request: "PUT /issues/seen?app=shop"},
		{name: "seen takes one row or --all", args: []string{"issues", "seen", "SHOP-3", "--all"}, login: true, code: 2, stderr: "specify one issue ID or --all", none: true},
		{name: "pr needs show", args: []string{"pr", "482"}, login: true, code: 2, stderr: "specify a command: show", none: true},
		{name: "a check for a pull request asks by the origin remote", args: []string{"check", "add", "--pull", "482"}, login: true, origin: "git@github.com:acme/shop.git\n", stdin: `{"title":"Gift card totals are right"}`, code: 0, stdout: `"id": "chk_2"`, stderr: "packrat check add: warning: it already passes on the 1h before now", request: `POST /checks?pull=482&repo=acme%2Fshop {"title":"Gift card totals are right"}`},
		{name: "a check needs a body", args: []string{"check", "add", "--pull", "482"}, login: true, code: 2, stderr: "on stdin as JSON", none: true},
		{name: "a check from a report", args: []string{"check", "add", "--app", "shop", "--report", "rep_1"}, login: true, stdin: `{"title":"It held"}`, code: 0, stdout: `"id": "chk_2"`, request: `POST /checks?app=shop&report=rep_1 {"title":"It held"}`},
		{name: "check with no command", args: []string{"check"}, login: true, code: 2, stderr: "Run packrat help check", none: true},
		{name: "check list puts open ones first", args: []string{"check", "list", "--app", "shop"}, login: true, code: 0,
			stdout: "chk_1\n—      closed 12 Oct 20:43  PR #541 (merge 93428404) makes the check skip"},
		{name: "check show reads the check by its id", args: []string{"check", "show", "chk_1"}, login: true, code: 0,
			stdout: "chk_1 · closed 2026-10-16 10:00 UTC\n\nAsked\n  x\n\nResult\n  Closed by a developer before it was verified.\n", request: "GET /checks/chk_1"},
		{name: "check show names a closed check's result", args: []string{"check", "show", "chk_2"}, login: true, code: 0,
			stdout: "chk_2 · passed 2026-10-16 10:05 UTC\n\nAsked\n  y\n\nResult\n  seen 199, matched 4\n"},
		{name: "check show of one that is not there", args: []string{"check", "show", "chk_0"}, login: true, code: 1, stderr: "packrat check show: No check chk_0 in your org."},
		{name: "check list", args: []string{"check", "list", "--app", "shop", "--pull", "482"}, login: true, code: 0, stdout: "=== shop/production checks\n\nPR     STATUS", request: "GET /checks?app=shop&pull=482"},
		{name: "no app, with more than one", args: []string{"check", "list"}, login: true, code: 2, stderr: "Error: No app specified. This key has access to claude-code and colorbook.\nUSAGE: packrat check list --app <app-name>\n"},
		{name: "check close", args: []string{"check", "close", "chk_1"}, login: true, code: 0, stdout: "Closed by a developer", request: "DELETE /checks/chk_1"},
		{name: "sql prints rows and a summary", args: []string{"sql", "--app", "shop", "select 1"}, login: true, code: 0, stdout: `"values":[1]`, stderr: "1 rows in 0.01s from shop/production", request: "POST /sql?app=shop select 1"},
		{name: "sql from stdin", args: []string{"sql"}, login: true, stdin: "select 2", code: 0, request: "POST /sql select 2"},
		{name: "sql with no script", args: []string{"sql"}, login: true, code: 2, stderr: "the query is empty", none: true},
		{name: "feedback sends the description from stdin", args: []string{"feedback", "--task", "connect the repository", "--kind", "broken", "--impact", "worked_around", "--about", "packrat status"}, login: true, origin: "git@github.com:acme/shop.git", stdin: "status --wait timed out\n", code: 0,
			stdout: "Sent to the Packrat team as fbk_5k2m9x0qv7ha.\n", request: `POST /feedback?repo=acme%2Fshop {"task":"connect the repository","kind":"broken","impact":"worked_around","about":"packrat status","description":"status --wait timed out"}`},
		{name: "feedback names the app it is given", args: []string{"feedback", "--task", "t", "--kind", "idea", "--impact", "friction", "--app", "shop/staging"}, login: true, stdin: "x", code: 0,
			stdout: "Sent to the Packrat team as fbk_5k2m9x0qv7ha.\n", request: `POST /feedback {"task":"t","kind":"idea","impact":"friction","app":"shop/staging","description":"x"}`},
		{name: "feedback says what the site refused", args: []string{"feedback", "--task", "open a pull request", "--kind", "idea", "--impact", "friction"}, login: true, stdin: "x", code: 1, stderr: "answered 429: too many reports from this key"},
		{name: "feedback needs a description", args: []string{"feedback", "--task", "t", "--kind", "idea", "--impact", "friction"}, login: true, stdin: " \n", code: 2, stderr: "provide the description on stdin", none: true},
		{name: "feedback needs a known kind", args: []string{"feedback", "--task", "t", "--kind", "praise", "--impact", "friction"}, login: true, stdin: "x", code: 2, stderr: "--kind is one of missing, broken, confusing, idea", none: true},
		{name: "feedback needs a known impact", args: []string{"feedback", "--task", "t", "--kind", "idea", "--impact", "none"}, login: true, stdin: "x", code: 2, stderr: "--impact is one of blocked, worked_around, friction", none: true},
		{name: "feedback needs a task", args: []string{"feedback", "--kind", "idea", "--impact", "friction"}, login: true, stdin: "x", code: 2, stderr: "--task names what you were doing", none: true},
		{name: "open a pasted curl line reads the report", args: []string{"open", `curl -sS QUERY/reports/rep_1 -H "Authorization: Bearer $(sed -nE 's/^PACKRAT_(API|QUERY)_KEY=//p' .env | tail -1)"`}, login: true, code: 0, stdout: "# Worked in part: #482", request: "GET /reports/rep_1"},
		{name: "open a page", args: []string{"open", "SITE/orgs/org_a/apps/shop/production/analyst"}, login: true, code: 0, stdout: "<h1>Analyst</h1>"},
		{name: "open a pull request", args: []string{"open", "https://github.com/acme/shop/pull/482"}, login: true, code: 0, stdout: "=== #482 Cache the cart total", request: "GET /pulls/482?repo=acme%2Fshop"},
		{name: "open names a page Packrat does not have", args: []string{"open", "SITE/nowhere"}, login: true, code: 1, stderr: "packrat open: there is no page at http://"},
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

// A request that runs SQL at Packrat waits as long as the query app does; any other gives up at the usual limit.
// A pull request's checks that were sent and not answered may be saved, and the message says how to look.
func TestSlowAnswers(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stdin  string
		slow   time.Duration
		code   int
		stdout string
		stderr string
	}{
		{name: "sql waits for a slow answer", args: []string{"sql", "--app", "shop", "select 1"}, slow: time.Second, code: 0},
		{name: "a pull request's checks wait for a slow answer", args: []string{"pr", "checks", "set", "482", "--file", "-"}, stdin: "[]", slow: time.Second, code: 0, stdout: "Declared 2 checks for #482"},
		{name: "checks that time out may be saved", args: []string{"pr", "checks", "set", "482", "--file", "-"}, stdin: "[]", slow: 50 * time.Millisecond, code: 1,
			stderr: "may still have saved the checks. Run packrat pr show 482 to see them before you send them again."},
		{name: "a command that runs no SQL keeps the short limit", args: []string{"apps"}, slow: time.Second, code: 1, stderr: "Client.Timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, _ := fakePackrat(t)
			target, err := url.Parse(fake.URL)
			if err != nil {
				t.Fatalf("parsing %s: %v", fake.URL, err)
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ErrorHandler = func(http.ResponseWriter, *http.Request, error) {}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			env := map[string]string{"PACKRAT_API_KEY": statusKey, "PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
			w := world{getenv: func(name string) string { return env[name] }, sleep: func(time.Duration) {}, store: auth.Store{Dir: t.TempDir()},
				stdin: strings.NewReader(tc.stdin), http: &http.Client{Timeout: 50 * time.Millisecond}, slow: &http.Client{Timeout: tc.slow},
				origin: func() string { return "git@github.com:acme/shop.git" }}
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
		})
	}
}

// packrat keys create is the one command that prints a key, and only the key it made.
func TestKeysCreate(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		key     string
		code    int
		stdout  string
		stderr  string
		request string
		none    bool
	}{
		{name: "shows the key it made once, with where to revoke it", args: []string{"keys", "create", "--name", "deploy agent"}, code: 0,
			stdout: "Made the API key \"deploy agent\" in Acme.\n\n  " + madeKey + "\n\nThis is the only time it's shown.", request: `POST /keys?repo=acme%2Fshop {"name":"deploy agent"}`},
		{name: "names the org's keys page", args: []string{"keys", "create", "--name", "deploy agent"}, code: 0, stdout: "/orgs/org_a/keys"},
		{name: "plain prints the key alone", args: []string{"keys", "create", "--name", "deploy agent", "--plain"}, code: 0, stdout: madeKey + "\n"},
		{name: "an org's key cannot make one", args: []string{"keys", "create", "--name", "deploy agent"}, key: statusKey, code: 1, stderr: "Run packrat login"},
		{name: "needs a name", args: []string{"keys", "create", "--name", " "}, code: 2, stderr: "--name says what the key is for", none: true},
		{name: "needs create", args: []string{"keys", "--name", "x"}, code: 2, stderr: "packrat keys create --name NAME", none: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asked := fakePackrat(t)
			env := map[string]string{"PACKRAT_API_KEY": tc.key, "PACKRAT_QUERY_URL": server.URL, "PACKRAT_URL": server.URL}
			store := auth.Store{Dir: t.TempDir()}
			if err := store.Save(auth.SignIn{Email: "dana@example.com", Key: "key_1", Token: loginKey}); err != nil {
				t.Fatalf("saving the sign-in: %v", err)
			}
			w := world{getenv: func(name string) string { return env[name] }, store: store, http: &http.Client{},
				origin: func() string { return "git@github.com:acme/shop.git" }}
			var stdout, stderr bytes.Buffer

			code := runIn(w, tc.args, &stdout, &stderr)

			if code != tc.code {
				t.Errorf("exit %d, want %d (stderr: %s)", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.stdout) || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stdout lacks %q or stderr lacks %q:\n%s%s", tc.stdout, tc.stderr, stdout.String(), stderr.String())
			}
			if slices.Contains(tc.args, "--plain") && code == 0 && stdout.String() != madeKey+"\n" {
				t.Errorf("--plain printed more than the key:\n%s", stdout.String())
			}
			for _, key := range []string{statusKey, loginKey} {
				if strings.Contains(stdout.String()+stderr.String(), key) {
					t.Errorf("the key that asked was printed:\n%s%s", stdout.String(), stderr.String())
				}
			}
			all := strings.Join(*asked, "\n")
			if tc.request != "" && !strings.Contains(all, tc.request) {
				t.Errorf("no request %q in:\n%s", tc.request, all)
			}
			if tc.none && len(*asked) > 0 {
				t.Errorf("asked Packrat when it must not:\n%s", all)
			}
		})
	}
}
