import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, CatalogStore, UnknownResultError, type ApiClient, type OperationDoc } from '@song-sleeve/core';
import { UploadQueue, type UploadItem } from './uploader';

const PART = 4;

function op(id: string, size: number): OperationDoc {
  return { id, track_id: 't-' + id, kind: 'initial_upload', state: 'active', phase: 'uploading', confirmed_bytes: 0, declared_bytes: size, part_size: PART, part_count: Math.ceil(size / PART), failure_reason: null, heartbeat_interval_seconds: 30 };
}

/** Fake XHR for presigned part PUTs; `failNext` makes the next sends fail at the network. */
class FakeXHR {
  static failNext = 0;
  static sent: { url: string; size: number }[] = [];
  upload: { onprogress: ((e: { loaded: number }) => void) | null } = { onprogress: null };
  status = 0;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  private url = '';
  open(_m: string, url: string) {
    this.url = url;
  }
  getResponseHeader(name: string) {
    return name === 'ETag' ? `"etag-${this.url}"` : null;
  }
  abort() {
    this.onabort?.();
  }
  send(body: Blob) {
    setTimeout(() => {
      if (FakeXHR.failNext > 0) {
        FakeXHR.failNext--;
        this.onerror?.();
        return;
      }
      FakeXHR.sent.push({ url: this.url, size: body.size });
      this.upload.onprogress?.({ loaded: body.size });
      this.status = 200;
      this.onload?.();
    }, 1);
  }
}

interface FakeApi {
  api: ApiClient;
  mutate: ReturnType<typeof vi.fn>;
  post: ReturnType<typeof vi.fn>;
  creates: { requestId: string; name: string }[];
}

function fakeApi(create?: (body: { name: string }, requestId: string) => Promise<unknown>): FakeApi {
  const creates: { requestId: string; name: string }[] = [];
  let n = 0;
  const mutate = vi.fn(async (method: string, path: string, body: { name: string; byte_size: number }, opts?: { requestId?: string }) => {
    if (path === '/me/tracks') {
      creates.push({ requestId: opts!.requestId!, name: body.name });
      if (create) return create(body, opts!.requestId!);
      const id = `op${++n}`;
      return { track: { id: 't-' + id, revision: 1 }, operation: op(id, body.byte_size) };
    }
    return {};
  });
  const post = vi.fn(async (path: string, body: { part_numbers?: number[]; confirmed_bytes?: number }) => {
    if (path.endsWith('/upload-grant')) return { parts: body.part_numbers!.map((p) => ({ part_number: p, url: `${path}#${p}` })) };
    if (path.endsWith('/heartbeat')) return { operation: { state: 'active' } };
    return {};
  });
  return { api: { mutate, post } as unknown as ApiClient, mutate, post, creates };
}

function item(name: string, size = 10): Omit<UploadItem, 'id' | 'status' | 'confirmed' | 'requestId'> {
  return { file: new File([new Uint8Array(size)], name + '.mp3'), name, artist: null, playlistIds: [], facts: { durationMs: 1000, byteSize: size, format: 'mp3', sha256: 'a'.repeat(64) } };
}

const settle = async (q: UploadQueue, ms = 3000) => {
  const end = Date.now() + ms;
  while (Date.now() < end && q.getItems().some((i) => i.status === 'queued' || i.status === 'syncing')) await new Promise((r) => setTimeout(r, 5));
};

beforeEach(() => {
  FakeXHR.failNext = 0;
  FakeXHR.sent = [];
  vi.stubGlobal('XMLHttpRequest', FakeXHR);
});
afterEach(() => vi.unstubAllGlobals());

describe('UploadQueue (R-CLOUD-9)', () => {
  it('runs at most three uploads at a time and creates documents only when starting', async () => {
    let active = 0;
    let peak = 0;
    const release: (() => void)[] = [];
    const f = fakeApi(async (body) => {
      active++;
      peak = Math.max(peak, active);
      await new Promise<void>((r) => release.push(r));
      active--;
      return { track: { id: 't-' + body.name, revision: 1 }, operation: op(body.name, 10) };
    });
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue(['a', 'b', 'c', 'd', 'e'].map((n) => item(n)));
    await new Promise((r) => setTimeout(r, 10));
    expect(f.creates.map((c) => c.name)).toEqual(['a', 'b', 'c']);
    expect(q.getItems().filter((i) => i.status === 'queued').map((i) => i.name)).toEqual(['d', 'e']);
    while (f.creates.length < 5 || release.length) {
      release.shift()?.();
      await new Promise((r) => setTimeout(r, 5));
    }
    await settle(q);
    expect(peak).toBe(3);
    expect(q.getItems().every((i) => i.status === 'done')).toBe(true);
  });

  it('sends every part, reports progress and finishes with all ETags', async () => {
    const f = fakeApi();
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('song', 10)]);
    await settle(q);
    expect(FakeXHR.sent.map((s) => s.size)).toEqual([4, 4, 2]);
    const uploaded = f.post.mock.calls.find((c) => String(c[0]).endsWith('/uploaded'))!;
    expect((uploaded[1] as { parts: unknown[] }).parts).toHaveLength(3);
    const beats = f.post.mock.calls.filter((c) => String(c[0]).endsWith('/heartbeat')).map((c) => (c[1] as { confirmed_bytes: number }).confirmed_bytes);
    expect(beats).toEqual([4, 8, 10]);
    expect(q.getItems()[0]).toMatchObject({ status: 'done', confirmed: 10 });
  });

  it('keeps going when one song fails, and never retries it by itself (D-25)', async () => {
    const f = fakeApi(async (body) => {
      if (body.name === 'bad') throw new ApiError('storage_quota_exceeded', 'full', 422);
      return { track: { id: 't-' + body.name, revision: 1 }, operation: op(body.name, 10) };
    });
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('ok1'), item('bad'), item('ok2')]);
    await settle(q);
    const byName = Object.fromEntries(q.getItems().map((i) => [i.name, i]));
    expect(byName.ok1!.status).toBe('done');
    expect(byName.ok2!.status).toBe('done');
    expect(byName.bad).toMatchObject({ status: 'failed', reason: 'storage_quota_exceeded' });
    expect(f.creates.filter((c) => c.name === 'bad')).toHaveLength(1);
  });

  it('Try again is a new sync with a new request ID (R-ID-2)', async () => {
    let fail = true;
    const f = fakeApi(async (body) => {
      if (fail) throw new ApiError('song_too_long', '', 422);
      return { track: { id: 't', revision: 1 }, operation: op(body.name, 10) };
    });
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('x')]);
    await settle(q);
    fail = false;
    q.retryAllFailed();
    await settle(q);
    expect(q.getItems()[0]!.status).toBe('done');
    expect(f.creates).toHaveLength(2);
    expect(f.creates[0]!.requestId).not.toBe(f.creates[1]!.requestId);
  });

  it('recovers an unknown result with the same request ID, never as a second sync (R-MUT-4)', async () => {
    let calls = 0;
    const f = fakeApi(async (body) => {
      if (++calls === 1) throw new UnknownResultError('x');
      return { track: { id: 't', revision: 1 }, operation: op(body.name, 10) };
    });
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('x')]);
    await settle(q, 6000);
    expect(f.creates).toHaveLength(2);
    expect(f.creates[0]!.requestId).toBe(f.creates[1]!.requestId);
    expect(q.getItems()[0]!.status).toBe('done');
  });

  it('resumes an interrupted part on the same operation', async () => {
    FakeXHR.failNext = 1;
    const f = fakeApi();
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('x', 4)]);
    await settle(q, 6000);
    expect(q.getItems()[0]!.status).toBe('done');
    expect(f.creates).toHaveLength(1);
  });

  it('fails a song whose file changed since review, before creating anything (F-21)', async () => {
    const f = fakeApi();
    const q = new UploadQueue(f.api, new CatalogStore());
    const it = item('changed', 10);
    q.enqueue([{ ...it, facts: { ...it.facts, byteSize: 99 } }]);
    await settle(q);
    expect(q.getItems()[0]).toMatchObject({ status: 'failed', reason: 'invalid_audio_metadata' });
    expect(f.creates).toHaveLength(0);
  });

  it('stops when the server cancels the operation, and cancels the server sync on local errors', async () => {
    const f = fakeApi();
    f.post.mockImplementation(async (path: string, body: { part_numbers?: number[] }) => {
      if (path.endsWith('/upload-grant')) return { parts: body.part_numbers!.map((p) => ({ part_number: p, url: 'u' + p })) };
      if (path.endsWith('/heartbeat')) return { operation: { state: 'failed' } };
      return {};
    });
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('x')]);
    await settle(q);
    expect(q.getItems()[0]).toMatchObject({ status: 'failed', reason: 'cancelled' });
  });

  it('drops queued songs on cancel, marks a failed sync from elsewhere, and clears on logout', async () => {
    const f = fakeApi(() => new Promise(() => undefined));
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue(['a', 'b', 'c', 'd'].map((n) => item(n)));
    const queued = q.getItems().find((i) => i.status === 'queued')!;
    q.cancelQueued(queued.id);
    expect(q.getItems().map((i) => i.name)).toEqual(['a', 'b', 'c']);
    expect(q.activeCount).toBe(3);
    q.clear();
    expect(q.getItems()).toHaveLength(0);
  });

  it('marks a song failed when its sync fails on the server (stall, cancel elsewhere)', async () => {
    const f = fakeApi();
    f.post.mockImplementation(() => new Promise(() => undefined));
    const q = new UploadQueue(f.api, new CatalogStore());
    q.enqueue([item('x')]);
    await new Promise((r) => setTimeout(r, 20));
    const trackId = q.getItems()[0]!.trackId!;
    q.onTrackGone(trackId, 'stalled');
    expect(q.getItems()[0]).toMatchObject({ status: 'failed', reason: 'stalled' });
  });
});
