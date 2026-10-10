OpenTelemetry::SDK.configure do |c|
  c.service_name = "shop"
  c.use_all
end
