package rails

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/packratlabs/packrat-cli/internal/detect"
)

func openFixture(t *testing.T, name string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("opening fixture %s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("closing fixture %s: %v", name, err)
		}
	})
	return root
}

func fixtureFile(t *testing.T, fixture, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..", "testdata", fixture)), name)
	if err != nil {
		t.Fatalf("reading %s of fixture %s: %v", name, fixture, err)
	}
	return data
}

func TestDetect(t *testing.T) {
	tests := []struct {
		fixture  string
		detected bool
		evidence detect.Evidence
	}{
		{"direct-and-transitive", true, detect.Evidence{Path: "Gemfile.lock", Line: 12, Text: "rails (8.0.2)"}},
		{"git-and-path-sources", true, detect.Evidence{Path: "Gemfile.lock", Line: 20, Text: "rails (7.2.2.1)"}},
		{"no-lockfile", true, detect.Evidence{Path: "Gemfile", Line: 3, Text: `gem "rails", "~> 8.0"`}},
		{"bundled-with-only", false, detect.Evidence{}},
		{"not-rails", false, detect.Evidence{}},
		{"empty", false, detect.Evidence{}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			detected, evidence := Stack{}.Detect(openFixture(t, tc.fixture))
			if detected != tc.detected || evidence != tc.evidence {
				t.Errorf("got %v %+v, want %v %+v", detected, evidence, tc.detected, tc.evidence)
			}
		})
	}
}

func TestStackVersions(t *testing.T) {
	tests := []struct {
		fixture, rails, ruby string
		notes                []string
	}{
		{"direct-and-transitive", "8.0.2", "3.4.2", nil},
		{"git-and-path-sources", "7.2.2.1", "3.2.4", nil},
		{"no-lockfile", "", "3.3.0", []string{noLockNote}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			facts, err := Stack{}.Read(openFixture(t, tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if facts.Stack.Version != tc.rails || facts.Stack.Ruby != tc.ruby {
				t.Errorf("got Rails %q Ruby %q, want Rails %q Ruby %q", facts.Stack.Version, facts.Stack.Ruby, tc.rails, tc.ruby)
			}
			if !reflect.DeepEqual(facts.Notes, tc.notes) {
				t.Errorf("notes %q, want %q", facts.Notes, tc.notes)
			}
		})
	}
}

type wantFinding struct {
	vendor, gem, version string
	direct, production   bool
	groups               []string
}

func TestFindings(t *testing.T) {
	tests := []struct {
		fixture string
		want    []wantFinding
	}{
		{"direct-and-transitive", []wantFinding{
			{"Sentry", "sentry-rails", "5.26.0", true, true, []string{}},
			{"Sentry", "sentry-ruby", "5.26.0", false, true, []string{}},
			{"logging", "lograge", "0.14.0", true, true, []string{}},
		}},
		{"datadog-test-group-only", []wantFinding{
			{"Datadog", "datadog", "2.9.0", true, false, []string{"test"}},
		}},
		{"git-and-path-sources", []wantFinding{
			{"Sentry", "sentry-ruby", "5.27.0", true, true, []string{}},
			{"logging", "lograge", "0.15.0.pre", true, true, []string{}},
		}},
		{"nested-groups", []wantFinding{
			{"Airbrake", "airbrake", "13.0.4", true, false, []string{"development", "test"}},
			{"AppSignal", "appsignal", "4.5.1", true, false, []string{"development", "test"}},
			{"Bugsnag", "bugsnag", "6.27.1", true, false, []string{"development", "test"}},
			{"Honeybadger", "honeybadger", "5.27.0", true, true, []string{"production", "staging"}},
			{"New Relic", "newrelic_rpm", "9.17.0", true, false, []string{"development"}},
			{"Rollbar", "rollbar", "3.6.1", true, false, []string{"development", "test"}},
			{"Scout", "scout_apm", "5.6.0", true, false, []string{"development", "test"}},
			{"Sentry", "sentry-ruby", "5.26.0", true, true, []string{}},
			{"Skylight", "skylight", "6.0.4", true, false, []string{"test"}},
			{"logging", "lograge", "0.14.0", true, true, []string{}},
		}},
		{"no-lockfile", []wantFinding{
			{"Sentry", "sentry-rails", "", true, true, []string{}},
			{"Skylight", "skylight", "", true, false, []string{"test"}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			facts, err := Stack{}.Read(openFixture(t, tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			got := []wantFinding{}
			for _, f := range facts.SDKs {
				got = append(got, wantFinding{f.Vendor, f.Gem, f.Version, f.Direct, f.Production(), f.Groups})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestLockfileSources(t *testing.T) {
	lock := ParseLockfile(fixtureFile(t, "git-and-path-sources", "Gemfile.lock"))
	for gem, source := range map[string]string{"sentry-ruby": "git", "lograge": "path", "rails": "gem"} {
		if lock.Gems[gem].Source != source {
			t.Errorf("%s has source %q, want %q", gem, lock.Gems[gem].Source, source)
		}
	}
}

func TestLockfileSkipsChecksums(t *testing.T) {
	lock := ParseLockfile(fixtureFile(t, "direct-and-transitive", "Gemfile.lock"))
	if _, found := lock.Gems["datadog"]; found {
		t.Error("a CHECKSUMS line was read as a gem")
	}
	if got := lock.Gems["nokogiri"].Version; got != "1.18.9" {
		t.Errorf("nokogiri version %q, want the version without a platform", got)
	}
}

func TestVendorTableHasNoDuplicateGem(t *testing.T) {
	seen := map[string]bool{}
	for _, row := range vendors() {
		for _, gem := range row.Gems {
			if seen[gem] {
				t.Errorf("%s is in the vendor table twice", gem)
			}
			seen[gem] = true
		}
	}
	if _, found := lookupVendor("opentelemetry-api"); found {
		t.Error("opentelemetry-api alone must not be a finding")
	}
}
