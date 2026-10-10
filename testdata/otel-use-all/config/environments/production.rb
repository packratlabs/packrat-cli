Rails.application.configure do
  config.x.otlp_endpoint = ENV["OTEL_EXPORTER_OTLP_ENDPOINT"]
end
