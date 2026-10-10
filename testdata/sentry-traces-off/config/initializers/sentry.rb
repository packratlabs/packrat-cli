# Sentry.init is described here; this comment is not the call.
Sentry.init do |config|
  config.dsn = ENV["SENTRY_DSN"]
  # config.traces_sample_rate = 1.0
  config.traces_sample_rate = nil
  config.enable_tracing = false
end
