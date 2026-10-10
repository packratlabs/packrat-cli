import * as Sentry from "@sentry/vue";

export function report(error) {
  Sentry.captureException(error);
}
