Sentry.init do |config|
  config.dsn = ENV['SENTRY_DSN']
end

Rails.application.config.after_initialize do
  Rails.error.subscribe(ErrorReporting::SentrySubscriber.new) if Sentry.initialized?
end
