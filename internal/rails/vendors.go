package rails

import "strings"

type vendor struct {
	Gems     []string
	Prefix   string
	Vendor   string
	Provides []string
	Note     string
}

// vendors is the whole vendor table, from section 4.4 of the plan. It is data. Do not add a row
// that the plan does not have.
func vendors() []vendor {
	return []vendor{
		{Gems: []string{"datadog", "ddtrace"}, Vendor: "Datadog", Provides: []string{"traces", "logs", "exceptions"}, Note: "needs an Agent; no agentless APM in Ruby"},
		{Gems: []string{"sentry-ruby", "sentry-rails", "sentry-sidekiq"}, Vendor: "Sentry", Provides: []string{"exceptions"}},
		{Gems: []string{"newrelic_rpm"}, Vendor: "New Relic", Provides: []string{"traces", "exceptions", "logs"}, Note: "coexists with OpenTelemetry"},
		{Gems: []string{"opentelemetry-sdk", "opentelemetry-exporter-otlp"}, Prefix: "opentelemetry-instrumentation-", Vendor: "OpenTelemetry", Provides: []string{"traces"}},
		{Gems: []string{"opentelemetry-logs-sdk", "opentelemetry-exporter-otlp-logs"}, Vendor: "OpenTelemetry", Provides: []string{"logs"}, Note: "pre-1.0"},
		{Gems: []string{"honeybadger"}, Vendor: "Honeybadger", Provides: []string{"exceptions"}},
		{Gems: []string{"bugsnag"}, Vendor: "Bugsnag", Provides: []string{"exceptions"}},
		{Gems: []string{"rollbar"}, Vendor: "Rollbar", Provides: []string{"exceptions"}},
		{Gems: []string{"airbrake"}, Vendor: "Airbrake", Provides: []string{"exceptions"}},
		{Gems: []string{"exception_notification"}, Vendor: "Exception Notification", Provides: []string{"exceptions"}},
		{Gems: []string{"scout_apm"}, Vendor: "Scout", Provides: []string{"traces"}},
		{Gems: []string{"skylight"}, Vendor: "Skylight", Provides: []string{"traces"}},
		{Gems: []string{"appsignal"}, Vendor: "AppSignal", Provides: []string{"traces"}},
		{Gems: []string{"elastic-apm"}, Vendor: "Elastic APM", Provides: []string{"traces"}},
		{Gems: []string{"lograge", "semantic_logger", "rails_semantic_logger", "logtail-rails", "logstash-logger"}, Vendor: "logging", Provides: []string{"logs shape"}, Note: "not a destination"},
		{Gems: []string{"posthog-ruby", "posthog-rails"}, Vendor: "PostHog", Provides: []string{"product analytics"}, Note: "out of scope; listed only"},
	}
}

func lookupVendor(gem string) (vendor, bool) {
	for _, row := range vendors() {
		for _, name := range row.Gems {
			if name == gem {
				return row, true
			}
		}
		if row.Prefix != "" && strings.HasPrefix(gem, row.Prefix) {
			return row, true
		}
	}
	return vendor{}, false
}
