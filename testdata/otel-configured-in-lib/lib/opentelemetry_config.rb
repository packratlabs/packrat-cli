module OpentelemetryConfig
  def self.setup
    OpenTelemetry::SDK.configure do |c|
      c.service_name = "shop"
    end
  end
end
