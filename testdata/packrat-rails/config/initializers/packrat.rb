# Sends your app's exceptions, traces and logs to Packrat: https://packrat.sh/docs/ruby-on-rails
Packrat.configure do |config|
  config.ingest_key = ENV.fetch("PACKRAT_INGEST_KEY", nil)
end
