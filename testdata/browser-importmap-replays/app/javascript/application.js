import * as Sentry from "@sentry/browser"

Sentry.init({
  dsn: document.querySelector("meta[name=sentry-dsn]").content,
  integrations: [Sentry.replayIntegration()],
})
