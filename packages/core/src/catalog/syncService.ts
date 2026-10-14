/**
 * The web's Device Sync Service (R-DEV-1, §7.9 step 2 only on the web): load the
 * snapshot, follow the event stream, and catch up from the cursor after every hint,
 * reconnect or return to the tab. Hints are never data (R-EVT-2).
 */
import { ApiError, type ApiClient } from '../api/client';
import type { Changes, Snapshot } from '../api/types';
import type { CatalogStore } from './store';

export type ConnectionState = 'connecting' | 'live' | 'offline';

export interface SyncServiceOptions {
  deviceId: () => string | null;
  onAccountChanged?: () => void;
  onAccountDeleted?: () => void;
  onConnectionChange?: (s: ConnectionState) => void;
}

export class CatalogSyncService {
  private abort: AbortController | null = null;
  private catchingUp: Promise<void> | null = null;
  private again = false;
  private stopped = false;
  private backoff = 1000;
  private conn: ConnectionState = 'connecting';
  private readonly onVisible = () => {
    if (document.visibilityState === 'visible') void this.catchUp();
  };
  private readonly onOnline = () => {
    this.backoff = 1000;
    this.reconnectSoon(0);
  };
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private readonly api: ApiClient,
    private readonly store: CatalogStore,
    private readonly opts: SyncServiceOptions,
  ) {}

  get connection(): ConnectionState {
    return this.conn;
  }

  private setConn(s: ConnectionState) {
    if (this.conn === s) return;
    this.conn = s;
    this.opts.onConnectionChange?.(s);
  }

  async start(): Promise<void> {
    this.stopped = false;
    await this.loadSnapshot();
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', this.onVisible);
    if (typeof window !== 'undefined') window.addEventListener('online', this.onOnline);
    void this.listen();
  }

  stop(): void {
    this.stopped = true;
    this.abort?.abort();
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', this.onVisible);
    if (typeof window !== 'undefined') window.removeEventListener('online', this.onOnline);
  }

  /** Full snapshot, all pages (§4.4). */
  async loadSnapshot(): Promise<void> {
    const pages: Snapshot[] = [];
    let token: string | undefined;
    do {
      const page = await this.api.get<Snapshot>('/me/catalog/snapshot' + (token ? `?page_token=${encodeURIComponent(token)}` : ''));
      pages.push(page);
      token = page.next_page_token;
    } while (token);
    this.store.loadSnapshot(pages);
  }

  /** Fetches changes after the cursor until caught up; coalesces concurrent calls. */
  catchUp(): Promise<void> {
    if (this.catchingUp) {
      this.again = true;
      return this.catchingUp;
    }
    this.catchingUp = (async () => {
      try {
        do {
          this.again = false;
          let more = true;
          while (more && !this.stopped) {
            const ch = await this.api.get<Changes>(`/me/catalog/changes?cursor=${this.store.getState().cursor}`);
            this.store.applyChanges(ch, this.opts.deviceId());
            if (ch.account_changed) this.opts.onAccountChanged?.();
            more = ch.has_more;
          }
        } while (this.again && !this.stopped);
      } catch (e) {
        if (e instanceof ApiError && e.code === 'cursor_expired') await this.loadSnapshot();
        else throw e;
      } finally {
        this.catchingUp = null;
      }
    })();
    return this.catchingUp;
  }

  private reconnectSoon(delay: number) {
    if (this.stopped) return;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.abort?.abort();
    this.reconnectTimer = setTimeout(() => void this.listen(), delay);
  }

  /** Reads the SSE stream with fetch so the client headers travel with it. */
  private async listen(): Promise<void> {
    if (this.stopped) return;
    this.abort?.abort();
    const abort = new AbortController();
    this.abort = abort;
    this.setConn('connecting');
    try {
      const res = await this.api.stream(`/me/events?cursor=${this.store.getState().cursor}`, abort.signal);
      if (res.status === 401 || res.status === 410) {
        if (res.status === 410) this.opts.onAccountDeleted?.();
        return;
      }
      if (!res.ok || !res.body) throw new Error('stream failed: ' + res.status);
      this.setConn('live');
      this.backoff = 1000;
      // Catch up once connected, in case hints were missed while away.
      void this.catchUp().catch(() => undefined);
      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      let buf = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += value;
        let idx: number;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          this.handle(block);
        }
      }
    } catch (e) {
      if (abort.signal.aborted && this.stopped) return;
    }
    if (this.stopped || abort.signal.aborted) return;
    // Reconnect with jitter and replay from the cursor (§10.2).
    this.setConn(typeof navigator !== 'undefined' && !navigator.onLine ? 'offline' : 'connecting');
    const delay = this.backoff + Math.random() * 1000;
    this.backoff = Math.min(this.backoff * 2, 30000);
    this.reconnectSoon(delay);
  }

  private handle(block: string) {
    let event = 'message';
    let data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      else if (line.startsWith('data:')) data += line.slice(5).trim();
    }
    if (!data && event === 'message') return;
    const payload = data ? JSON.parse(data) : {};
    switch (event) {
      case 'changes':
        void this.catchUp().catch(() => undefined);
        break;
      case 'progress':
        this.store.setProgress(payload.track_id, payload.confirmed_bytes, payload.declared_bytes);
        break;
      case 'account_deleted':
        this.opts.onAccountDeleted?.();
        break;
      case 'reconnect':
        this.backoff = 1000;
        break;
    }
  }
}
