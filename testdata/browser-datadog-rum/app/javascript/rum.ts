import { datadogRum } from "@datadog/browser-rum"

datadogRum.init({ applicationId: "app", clientToken: "pub", sessionReplaySampleRate: 20 })
