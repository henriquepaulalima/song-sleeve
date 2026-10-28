import { describe, expect, it } from 'vitest';
import cases from './scrub-cases.json';
import { scrubEvent, scrubText } from './scrub';

describe('scrubText', () => {
  it.each(cases)('$in', ({ in: input, out }) => {
    expect(scrubText(input)).toBe(out);
  });
});

describe('scrubEvent', () => {
  it('keeps the crash and drops personal data', () => {
    const e = scrubEvent({
      event_id: 'e1',
      level: 'error',
      release: '0.1.0',
      message: 'failed on "Low Tide"',
      user: { email: 'alex@example.com' },
      request: { url: 'wails://wails/#/playlists/123' },
      breadcrumbs: [{ message: 'clicked Road Trip' }],
      tags: { song: 'Low Tide' },
      extra: { path: '/home/alex/Music' },
      contexts: { os: { name: 'Linux' }, state: { song: 'Low Tide' } },
      exception: {
        values: [
          {
            type: 'TypeError',
            value: "Cannot read 'name' of /home/alex/Music/a.mp3",
            stacktrace: { frames: [{ filename: 'wails://wails/assets/index-abc.js?x=1', function: 'play', lineno: 3, colno: 9, vars: { name: 'Low Tide' } }] },
          },
        ],
      },
    });
    expect(e).toEqual({
      event_id: 'e1',
      level: 'error',
      release: '0.1.0',
      message: 'failed on [text]',
      contexts: { os: { name: 'Linux' } },
      exception: {
        values: [
          {
            type: 'TypeError',
            value: 'Cannot read [text] of [path]',
            mechanism: undefined,
            stacktrace: { frames: [{ filename: 'index-abc.js', function: 'play', lineno: 3, colno: 9, in_app: undefined }] },
          },
        ],
      },
    });
  });
});
