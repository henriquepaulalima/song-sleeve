/**
 * Removes personal and song data before anything goes to crash reporting (D-35,
 * §4.4): song, artist, playlist and group names (which error messages quote),
 * file paths, emails, URLs and tokens. The Go twin is server/pkg/scrub; both run
 * scrub-cases.json.
 */

const RULES: [RegExp, string][] = [
  // URLs first: they contain paths and sometimes emails.
  [/[a-zA-Z][a-zA-Z0-9+.-]*:\/\/[^\s"'<>]+/g, '[url]'],
  [/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g, '[email]'],
  // Quoted names: "…", '…', “…”, ‘…’.
  [/"[^"\n]*"|'[^'\n]*'|“[^”\n]*”|‘[^’\n]*’/g, '[text]'],
  // Paths, which may contain spaces, up to a colon, a quote or the end.
  [/[A-Za-z]:\\[^:"'\n]*[^\s:"'\n]/g, '[path]'],
  [/(?:~|\/[^\s/:"']+)(?:\/[^/:"'\n]*[^\s/:"'\n])+/g, '[path]'],
  // Tokens and hashes: long base64url or hex runs (IDs with dashes are kept).
  [/\b[A-Za-z0-9_]{24,}\b/g, '[token]'],
];

export function scrubText(s: string): string {
  for (const [re, w] of RULES) s = s.replace(re, w);
  return s;
}

/** The parts of a Sentry event this app sends; anything else is dropped. */
interface Frame {
  filename?: string;
  function?: string;
  lineno?: number;
  colno?: number;
  in_app?: boolean;
  [k: string]: unknown;
}
interface CrashEvent {
  message?: string;
  exception?: { values?: { type?: string; value?: string; stacktrace?: { frames?: Frame[] }; mechanism?: unknown }[] };
  [k: string]: unknown;
}

const KEEP = new Set(['event_id', 'timestamp', 'platform', 'level', 'release', 'environment', 'sdk', 'message', 'exception', 'contexts', 'type']);
const KEEP_CONTEXTS = new Set(['os', 'browser', 'runtime']);

/**
 * Keeps only what explains a crash: the error type, its scrubbed message and a
 * stack of file names (not paths or URLs) and lines. No user, request, URLs,
 * breadcrumbs, tags or extra data.
 */
export function scrubEvent<E extends object>(input: E): E {
  const event = input as CrashEvent;
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(event)) if (KEEP.has(k)) out[k] = v;
  if (typeof out.message === 'string') out.message = scrubText(out.message);
  if (out.contexts && typeof out.contexts === 'object') {
    out.contexts = Object.fromEntries(Object.entries(out.contexts).filter(([k]) => KEEP_CONTEXTS.has(k)));
  }
  if (event.exception?.values) {
    out.exception = {
      values: event.exception.values.map((v) => ({
        type: v.type,
        value: v.value === undefined ? undefined : scrubText(v.value),
        mechanism: v.mechanism,
        stacktrace: v.stacktrace?.frames
          ? {
              frames: v.stacktrace.frames.map((f) => ({
                filename: f.filename?.split(/[/\\]/).pop()?.split('?')[0],
                function: f.function,
                lineno: f.lineno,
                colno: f.colno,
                in_app: f.in_app,
              })),
            }
          : undefined,
      })),
    };
  }
  return out as E;
}
