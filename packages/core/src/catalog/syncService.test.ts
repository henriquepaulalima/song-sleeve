import { describe, expect, it, vi } from 'vitest';
import { ApiError, type ApiClient } from '../api/client';
import type { Changes, Snapshot } from '../api/types';
import { CatalogStore } from './store';
import { CatalogSyncService } from './syncService';

const changes = (cursor: number, more = false, extra: Partial<Changes> = {}): Changes => ({ cursor, has_more: more, events: [], account_changed: false, deleted: {}, ...extra });

function sseResponse(text: string, keepOpen = true) {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(ctl) {
      ctl.enqueue(enc.encode(text));
      if (!keepOpen) ctl.close();
    },
  });
  return new Response(body, { status: 200 });
}

function fakeApi(routes: Record<string, unknown[]>, stream: () => Promise<Response>) {
  const calls: string[] = [];
  const api = {
    get: vi.fn(async (path: string) => {
      calls.push(path);
      const key = Object.keys(routes).find((k) => path.startsWith(k))!;
      const next = routes[key]!.shift();
      if (next instanceof Error) throw next;
      return next;
    }),
    stream: vi.fn(stream),
  } as unknown as ApiClient;
  return { api, calls };
}

const flush = () => new Promise((r) => setTimeout(r, 20));

describe('CatalogSyncService', () => {
  it('loads every snapshot page', async () => {
    const pages: Snapshot[] = [{ cursor: 3, tracks: [], next_page_token: 'p2', artists: [], playlists: [], groups: [], scopes: [] }, { tracks: [] }];
    const { api, calls } = fakeApi({ '/me/catalog/snapshot': pages }, () => new Promise(() => undefined));
    const store = new CatalogStore();
    await new CatalogSyncService(api, store, { deviceId: () => 'd' }).loadSnapshot();
    expect(calls).toEqual(['/me/catalog/snapshot', '/me/catalog/snapshot?page_token=p2']);
    expect(store.getState().cursor).toBe(3);
  });

  it('catches up page by page from the cursor', async () => {
    const { api, calls } = fakeApi({ '/me/catalog/changes': [changes(5, true), changes(9)] }, () => new Promise(() => undefined));
    const store = new CatalogStore();
    store.loadSnapshot([{ cursor: 2, tracks: [] }]);
    const onAccountChanged = vi.fn();
    const svc = new CatalogSyncService(api, store, { deviceId: () => null, onAccountChanged });
    await svc.catchUp();
    expect(calls).toEqual(['/me/catalog/changes?cursor=2', '/me/catalog/changes?cursor=5']);
    expect(store.getState().cursor).toBe(9);
    expect(onAccountChanged).not.toHaveBeenCalled();
  });

  it('reloads the snapshot when the cursor expired (R-EVT-4)', async () => {
    const { api, calls } = fakeApi(
      { '/me/catalog/changes': [new ApiError('cursor_expired', '', 410)], '/me/catalog/snapshot': [{ cursor: 50, tracks: [] }] },
      () => new Promise(() => undefined),
    );
    const store = new CatalogStore();
    await new CatalogSyncService(api, store, { deviceId: () => null }).catchUp();
    expect(calls).toEqual(['/me/catalog/changes?cursor=0', '/me/catalog/snapshot']);
    expect(store.getState().cursor).toBe(50);
  });

  it('follows the stream: changes hints trigger a catch-up, progress hints update progress', async () => {
    const { api, calls } = fakeApi(
      { '/me/catalog/snapshot': [{ cursor: 1, tracks: [] }], '/me/catalog/changes': [changes(1), changes(4, false, { account_changed: true }), changes(4)] },
      async () => sseResponse('event: ready\ndata: {"cursor":1}\n\nevent: changes\ndata: {"cursor":4}\n\nevent: progress\ndata: {"track_id":"t","confirmed_bytes":10,"declared_bytes":40}\n\n'),
    );
    const store = new CatalogStore();
    const onConnectionChange = vi.fn();
    const onAccountChanged = vi.fn();
    const svc = new CatalogSyncService(api, store, { deviceId: () => null, onConnectionChange, onAccountChanged });
    await svc.start();
    await flush();
    expect(onConnectionChange).toHaveBeenCalledWith('live');
    expect(calls.filter((c) => c.startsWith('/me/catalog/changes')).length).toBeGreaterThanOrEqual(2);
    expect(store.getState().cursor).toBe(4);
    expect(onAccountChanged).toHaveBeenCalled();
    expect(store.getState().progress.get('t')).toMatchObject({ confirmed: 10, declared: 40 });
    svc.stop();
  });

  it('reports account deletion from the stream', async () => {
    const { api } = fakeApi({ '/me/catalog/snapshot': [{ cursor: 1, tracks: [] }], '/me/catalog/changes': [changes(1)] }, async () => sseResponse('event: account_deleted\ndata: {}\n\n'));
    const onAccountDeleted = vi.fn();
    const svc = new CatalogSyncService(api, new CatalogStore(), { deviceId: () => null, onAccountDeleted });
    await svc.start();
    await flush();
    expect(onAccountDeleted).toHaveBeenCalled();
    svc.stop();
  });

  it('reconnects after the stream ends', async () => {
    vi.useFakeTimers();
    let opened = 0;
    const { api } = fakeApi({ '/me/catalog/snapshot': [{ cursor: 1, tracks: [] }], '/me/catalog/changes': Array.from({ length: 10 }, () => changes(1)) }, async () => {
      opened++;
      return sseResponse('event: ready\ndata: {}\n\n', false);
    });
    const svc = new CatalogSyncService(api, new CatalogStore(), { deviceId: () => null });
    await svc.start();
    await vi.advanceTimersByTimeAsync(5000);
    expect(opened).toBeGreaterThanOrEqual(2);
    svc.stop();
    vi.useRealTimers();
  });
});
