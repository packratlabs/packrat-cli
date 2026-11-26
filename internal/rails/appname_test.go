package rails

import "testing"

func TestAppName(t *testing.T) {
	cases := []struct{ name, file, want string }{
		{"one word", "require_relative \"boot\"\n\nmodule Glass\n  class Application < Rails::Application\n  end\nend\n", "glass"},
		{"camel case", "module PackratRailsBlank\n  class Application < Rails::Application\n", "packrat_rails_blank"},
		{"an acronym", "module HTTPServer\n  class Application < Rails::Application\n", "http_server"},
		{"a digit", "module App2Go\n  class Application < Rails::Application\n", "app2_go"},
		{"a nested module names the innermost", "module Acme::Shop\n  class Application < Rails::Application\n", "shop"},
		{"a module named in a comment is not the app's", "# module Other\nmodule Shop\n", "shop"},
		{"no module", "Rails.application.configure do\nend\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AppName([]byte(c.file)); got != c.want {
				t.Fatalf("AppName = %q, want %q", got, c.want)
			}
		})
	}
}
