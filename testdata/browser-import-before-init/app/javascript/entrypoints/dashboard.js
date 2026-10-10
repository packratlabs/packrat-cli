import { createApp } from "vue";
import * as Sentry from "@sentry/vue";

const app = createApp({});
Sentry.init({ app, dsn: window.sentryDsn });
