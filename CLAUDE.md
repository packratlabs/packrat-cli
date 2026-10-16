# packrat-cli

Report to the owner in ASD-STE100 Simplified Technical English. Short sentences, one idea each.

`packrat detect` reads a repository and says what observability it has. `packrat plan` says how
Packrat would take each signal. `detect` prints facts only; a suggestion, a state or a step belongs
in `plan` (`internal/report/plan.go`). Both are read-only: no network call, no file written, no
environment value read. These
three are promises checked by tests (`internal/detect/canary_test.go`, `nonetwork_test.go`); do not
weaken a test to make a change pass.

`login`, `logout`, `auth`, `pr`, `watch`, `open`, `sql`, `apps` and `status` are the other half:
they ask Packrat. `internal/query` is the only package that sends a request. The key is
`PACKRAT_API_KEY` from the environment, else the sign-in that `internal/auth` keeps (the macOS
Keychain, or `credentials.json`); no env file is read for it. A key must never reach stdout, stderr
or an error; `main_test.go`, `commands_test.go` and `internal/query/query_test.go` check this. The key
goes only to a Packrat host (`query.Page` checks the host of an address it did not build). `detect`
must not import `internal/query`. `plan` asks the API only with `--app`.

Go, standard library only. `make check` (gofmt, vet, staticcheck, golangci-lint, tests) must be
green before every commit. No `panic`, `log.Fatal` or `os.Exit` outside `main.go`. Every error is
wrapped with context. No package-level mutable state. Tests are table-driven on fixtures in
`testdata/`; a new behaviour gets a new fixture named for it.

In `detect`, env files are read for variable **names only**: split at the first `=`, keep the left, discard the
right at once, and never put a line of such a file into evidence text.

The rules for which route a signal takes, which `plan` prints, are in `internal/rails/routes.go`, numbered as in the
plan (`~/Downloads/packrat-cli-plan.md`, section 5.1). Change a rule there and in its table test
together.

Do not commit, push, tag or open a pull request unless told to. End each milestone with a short
report: built, tested, left out, and the commands to see it.

Comment only what the code cannot say.
