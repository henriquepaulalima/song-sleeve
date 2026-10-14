/**
 * The tab's sync queue (R-CLOUD-9, §7.2–§7.3): songs wait as `queued`, and three
 * workers take them in turn. A worker creates the track document only when it
 * starts the song, so only uploading songs are visible elsewhere. Each song is
 * independent; a failure never stops the others and is never retried
 * automatically (D-25). The queue lives in memory only (R-WEB).
 */
import {
  ApiError,
  ConnectionRequiredError,
  UnknownResultError,
  newRequestId,
  type ApiClient,
  type ArtistRef,
  type CatalogStore,
  type OperationDoc,
  type Track,
} from '@song-sleeve/core';

export interface UploadFacts {
  durationMs: number;
  byteSize: number;
  format: 'mp3' | 'aac' | 'flac' | 'wav';
  sha256: string;
}

export interface UploadItem {
  id: string;
  file: File;
  name: string;
  artist: ArtistRef | null;
  playlistIds: string[];
  facts: UploadFacts;
  status: 'queued' | 'syncing' | 'failed' | 'done';
  confirmed: number;
  trackId?: string;
  operationId?: string;
  reason?: string;
  /** Kept across network retries so POST /me/tracks stays idempotent. */
  requestId: string;
}

export interface Replacement {
  trackId: string;
  file: File;
  facts: UploadFacts;
  op: OperationDoc;
}

const CONCURRENCY = 3;
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function failureReason(e: unknown): string {
  if (e instanceof ApiError) {
    if (['storage_quota_exceeded', 'song_too_long', 'song_too_large', 'unsupported_format', 'invalid_audio_metadata', 'cloud_sync_unavailable'].includes(e.code)) return e.code;
    if (e.code === 'operation_not_active' || e.code === 'not_found') return 'cancelled';
  }
  return 'upload_error';
}

/** PUTs one part through a presigned grant with progress; resolves to its ETag. */
function putPart(url: string, body: Blob, onProgress: (sent: number) => void, signal: AbortSignal): Promise<string> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', url);
    xhr.upload.onprogress = (e) => onProgress(e.loaded);
    xhr.onload = () => {
      const etag = xhr.getResponseHeader('ETag');
      if (xhr.status >= 200 && xhr.status < 300 && etag) resolve(etag);
      else reject(new Error(`part upload failed: ${xhr.status}`));
    };
    xhr.onerror = () => reject(new Error('network'));
    xhr.onabort = () => reject(new DOMException('Aborted', 'AbortError'));
    signal.addEventListener('abort', () => xhr.abort(), { once: true });
    xhr.send(body);
  });
}

/**
 * Sends a file's parts for an active operation, with heartbeats and retries of
 * interrupted parts on the same operation (R-ID-4: resuming a part is not a retry).
 */
async function sendFile(api: ApiClient, op: OperationDoc, file: File, onProgress: (confirmed: number) => void, signal: AbortSignal): Promise<void> {
  const parts: { part_number: number; etag: string }[] = [];
  let confirmed = 0;
  let lastBeat = 0;
  const beat = async (force = false) => {
    if (!force && Date.now() - lastBeat < op.heartbeat_interval_seconds * 1000) return;
    lastBeat = Date.now();
    const r = await api.post<{ operation: OperationDoc }>(`/me/operations/${op.id}/heartbeat`, { confirmed_bytes: confirmed });
    if (r.operation.state !== 'active') throw new ApiError('operation_not_active', 'Upload cancelled', 409);
  };
  const timer = setInterval(() => void beat().catch(() => undefined), op.heartbeat_interval_seconds * 1000);
  try {
    for (let n = 1; n <= op.part_count; n++) {
      const start = (n - 1) * op.part_size;
      const chunk = file.slice(start, Math.min(start + op.part_size, file.size));
      for (let attempt = 0; ; attempt++) {
        if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
        try {
          const g = await api.post<{ parts: { part_number: number; url: string }[] }>(`/me/operations/${op.id}/upload-grant`, { part_numbers: [n] });
          const etag = await putPart(g.parts[0]!.url, chunk, (sent) => onProgress(confirmed + sent), signal);
          parts.push({ part_number: n, etag });
          confirmed += chunk.size;
          onProgress(confirmed);
          await beat(true);
          break;
        } catch (e) {
          if ((e as Error).name === 'AbortError' || (e instanceof ApiError && e.code !== 'rate_limited')) throw e;
          // Network errors retry on the same operation, within the stall window.
          if (attempt >= 8) throw e;
          await sleep(Math.min(30_000, 1000 * 2 ** attempt));
        }
      }
    }
    for (let attempt = 0; ; attempt++) {
      try {
        await api.post(`/me/operations/${op.id}/uploaded`, { parts });
        return;
      } catch (e) {
        if (e instanceof ApiError || attempt >= 5) throw e;
        await sleep(1000 * 2 ** attempt);
      }
    }
  } finally {
    clearInterval(timer);
  }
}

export class UploadQueue {
  private items: UploadItem[] = [];
  private running = new Map<string, AbortController>();
  private listeners = new Set<() => void>();
  private snapshot: readonly UploadItem[] = [];

  constructor(
    private readonly api: ApiClient,
    private readonly catalog: CatalogStore,
  ) {}

  getItems = (): readonly UploadItem[] => this.snapshot;

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  private emit() {
    this.snapshot = [...this.items];
    for (const l of this.listeners) l();
  }

  private update(id: string, patch: Partial<UploadItem>) {
    this.items = this.items.map((i) => (i.id === id ? { ...i, ...patch } : i));
    this.emit();
  }

  get activeCount(): number {
    return this.items.filter((i) => i.status === 'queued' || i.status === 'syncing').length;
  }

  enqueue(items: Omit<UploadItem, 'id' | 'status' | 'confirmed' | 'requestId'>[]): void {
    for (const it of items) this.items.push({ ...it, id: crypto.randomUUID(), status: 'queued', confirmed: 0, requestId: newRequestId() });
    this.emit();
    this.pump();
  }

  /** Try again: a new sync with a new track ID (R-ID-2). */
  retry(id: string): void {
    const it = this.items.find((i) => i.id === id);
    if (!it || it.status !== 'failed') return;
    this.update(id, { status: 'queued', confirmed: 0, reason: undefined, trackId: undefined, operationId: undefined, requestId: newRequestId() });
    this.pump();
  }

  retryAllFailed(): void {
    for (const it of this.items.filter((i) => i.status === 'failed')) this.retry(it.id);
  }

  dismiss(id: string): void {
    this.items = this.items.filter((i) => i.id !== id || i.status === 'syncing');
    this.emit();
  }

  /** Cancel a song still waiting in this tab's queue (the server knows nothing about it). */
  cancelQueued(id: string): void {
    this.items = this.items.filter((i) => !(i.id === id && i.status === 'queued'));
    this.emit();
  }

  /** Stops local work for a track whose sync was cancelled or failed elsewhere. */
  onTrackGone(trackId: string, reason: string): void {
    const it = this.items.find((i) => i.trackId === trackId && i.status === 'syncing');
    if (!it) return;
    this.running.get(it.id)?.abort();
    this.update(it.id, { status: 'failed', reason });
  }

  clear(): void {
    for (const a of this.running.values()) a.abort();
    this.running.clear();
    this.items = [];
    this.emit();
  }

  private pump() {
    while (this.running.size < CONCURRENCY) {
      const next = this.items.find((i) => i.status === 'queued' && !this.running.has(i.id));
      if (!next) return;
      const abort = new AbortController();
      this.running.set(next.id, abort);
      this.update(next.id, { status: 'syncing' });
      void this.run(next.id, abort.signal).finally(() => {
        this.running.delete(next.id);
        this.pump();
      });
    }
  }

  private async run(id: string, signal: AbortSignal): Promise<void> {
    let item = this.items.find((i) => i.id === id)!;
    try {
      // Pre-check (F-21): the picked file must still match the facts reviewed.
      if (item.file.size !== item.facts.byteSize) throw new ApiError('invalid_audio_metadata', 'File changed', 422);
      let created: { track: Track; operation: OperationDoc } | null = null;
      for (let attempt = 0; !created; attempt++) {
        try {
          created = await this.api.mutate<{ track: Track; operation: OperationDoc }>(
            'POST',
            '/me/tracks',
            {
              name: item.name,
              artist: item.artist,
              duration_ms: item.facts.durationMs,
              byte_size: item.facts.byteSize,
              format: item.facts.format,
              sha256: item.facts.sha256,
              playlist_ids: item.playlistIds,
            },
            { requestId: item.requestId },
          );
        } catch (e) {
          // An unknown result is recovered with the same request ID, never as a
          // new sync (R-MUT-4).
          if (!(e instanceof UnknownResultError || e instanceof ConnectionRequiredError) || attempt >= 6) throw e;
          await sleep(Math.min(30_000, 1500 * 2 ** attempt));
        }
      }
      this.catalog.upsertTrack(created.track);
      this.update(id, { trackId: created.track.id, operationId: created.operation.id });
      item = this.items.find((i) => i.id === id)!;
      await sendFile(this.api, created.operation, item.file, (c) => this.update(id, { confirmed: c }), signal);
      this.update(id, { status: 'done', confirmed: item.facts.byteSize });
      // Done items leave the list once the catalog shows the published track.
      setTimeout(() => this.dismissDone(id), 4000);
    } catch (e) {
      if ((e as Error).name === 'AbortError') return;
      const cur = this.items.find((i) => i.id === id);
      if (cur?.status === 'failed') return;
      // A track document that was created stays until the server fails it by
      // cancel or stall; cancel it now so other devices drop it at once.
      if (cur?.operationId && !(e instanceof ApiError && e.code === 'operation_not_active')) {
        void this.api.mutate('POST', `/me/operations/${cur.operationId}/cancel`).catch(() => undefined);
      }
      this.update(id, { status: 'failed', reason: failureReason(e) });
    }
  }

  private dismissDone(id: string) {
    this.items = this.items.filter((i) => !(i.id === id && i.status === 'done'));
    this.emit();
  }

  /** Uploads a replacement file for an active replace operation (§7.7). */
  async replace(r: Replacement, onProgress: (confirmed: number) => void, signal: AbortSignal): Promise<void> {
    await sendFile(this.api, r.op, r.file, onProgress, signal);
  }
}
