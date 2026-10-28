/**
 * Crash reports from the page (D-35): scrubbed here, sent through the shell's
 * tunnel (/local/sentry), which drops them when reports are off and only forwards
 * to this build's DSN. No breadcrumbs, sessions, user or URLs.
 */
import * as Sentry from '@sentry/browser';
import { scrubEvent } from '@song-sleeve/core';
import type { ShellState } from './bridge';

let enabled = false;

// Only what's needed to catch and describe errors.
const KEEP = new Set(['InboundFilters', 'FunctionToString', 'BrowserApiErrors', 'GlobalHandlers', 'LinkedErrors', 'Dedupe']);

export function initCrashReports(c: ShellState['crash']) {
  enabled = c.enabled;
  if (!c.dsn) return;
  Sentry.init({
    dsn: c.dsn,
    release: c.release,
    environment: c.environment,
    tunnel: '/local/sentry',
    // Collect nothing about the user or requests; scrubEvent drops it anyway.
    dataCollection: { userInfo: false, cookies: false, httpHeaders: false, httpBodies: [], urlQueryParams: false },
    maxBreadcrumbs: 0,
    integrations: (defaults) => defaults.filter((i) => KEEP.has(i.name)),
    beforeSend: (event) => (enabled ? scrubEvent(event) : null),
  });
  // Development (SONG_SLEEVE_DEV_CRASH_TEST=1): one test error, with a fake name
  // and path to show the scrubbing.
  if (c.test) Sentry.captureException(new Error('crash test from the page: "Test Song" at /home/test/Music/Test Song.mp3'));
}

export function setCrashReports(on: boolean) {
  enabled = on;
}
