import type { ErrorCode, Platform } from './types';

/** An error the server answered with (the error envelope, §4.4). */
export class ApiError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string,
    readonly status: number,
    readonly details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/**
 * The request never reached the server (R-MUT-1): "Connect to the server to make
 * changes. This change was not saved."
 */
export class ConnectionRequiredError extends ApiError {
  constructor() {
    super('connection_required', 'Connect to the server to make changes. This change was not saved.', 0);
    this.name = 'ConnectionRequiredError';
  }
}

/**
 * A change was sent but its result is unknown (R-MUT-4). The request ID is kept and
 * recovered after reconnecting; the change is never resubmitted as a new action.
 */
export class UnknownResultError extends Error {
  constructor(readonly requestId: string) {
    super('Could not confirm this change. Reconnect to check its status.');
    this.name = 'UnknownResultError';
  }
}

export interface ClientOptions {
  baseUrl: string;
  platform: Platform;
  version: string;
  /** Bearer token for installed apps; the web uses its session cookie. */
  token?: () => string | null;
  /** Called on 401 so the app can return to Login (web) or ask to sign in again. */
  onUnauthenticated?: () => void;
  onUpdateRequired?: (minimum: string | undefined) => void;
  isOnline?: () => boolean;
  /** Replaces the global fetch (the desktop shell maps its proxy answers). */
  fetch?: typeof fetch;
}

export interface MutateOptions {
  requestId?: string;
}

export interface OutstandingRequest {
  requestId: string;
  method: string;
  path: string;
  at: number;
}

export type MutationStatus =
  | { status: 'committed' | 'rejected'; http_status: number; result: unknown; operation_id: string | null }
  | { status: 'not_applied' };

export function newRequestId(): string {
  return crypto.randomUUID();
}

export class ApiClient {
  private csrf: string | null = null;
  /** Requests whose result is unknown (R-MUT-4). In memory only on the web (R-WEB). */
  readonly outstanding = new Map<string, OutstandingRequest>();

  private readonly fetch: typeof fetch;

  constructor(private readonly opts: ClientOptions) {
    this.fetch = opts.fetch ?? ((input, init) => fetch(input, init));
  }

  setCsrfToken(token: string | null | undefined): void {
    this.csrf = token ?? null;
  }

  get platform(): Platform {
    return this.opts.platform;
  }

  private headers(extra: Record<string, string> = {}): Headers {
    const h = new Headers(extra);
    h.set('X-Client-Platform', this.opts.platform);
    h.set('X-Client-Version', this.opts.version);
    const token = this.opts.token?.();
    if (token) h.set('Authorization', `Bearer ${token}`);
    if (this.csrf) h.set('X-CSRF-Token', this.csrf);
    return h;
  }

  private async parse<T>(res: Response): Promise<T> {
    if (res.status === 204) return undefined as T;
    const text = await res.text();
    const body = text ? JSON.parse(text) : undefined;
    if (!res.ok) {
      const e = body?.error ?? {};
      const err = new ApiError((e.code ?? 'internal') as ErrorCode, e.message ?? res.statusText, res.status, e.details ?? {});
      if (err.code === 'unauthenticated') this.opts.onUnauthenticated?.();
      if (err.code === 'client_update_required') this.opts.onUpdateRequired?.(e.details?.minimum_version as string | undefined);
      throw err;
    }
    return body as T;
  }

  private offline(): boolean {
    const online = this.opts.isOnline?.() ?? (typeof navigator !== 'undefined' ? navigator.onLine : true);
    return !online;
  }

  /** A read. Network failures surface as connection_required. */
  async get<T>(path: string, init: { signal?: AbortSignal } = {}): Promise<T> {
    if (this.offline()) throw new ConnectionRequiredError();
    let res: Response;
    try {
      res = await this.fetch(this.opts.baseUrl + path, { method: 'GET', headers: this.headers(), credentials: 'include', signal: init.signal });
    } catch (e) {
      if ((e as Error).name === 'AbortError') throw e;
      throw new ConnectionRequiredError();
    }
    return this.parse<T>(res);
  }

  /** A non-mutation POST (auth flows, grants, heartbeats). */
  async post<T>(path: string, body?: unknown, init: { signal?: AbortSignal } = {}): Promise<T> {
    if (this.offline()) throw new ConnectionRequiredError();
    let res: Response;
    try {
      res = await this.fetch(this.opts.baseUrl + path, {
        method: 'POST',
        headers: this.headers({ 'Content-Type': 'application/json' }),
        credentials: 'include',
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: init.signal,
      });
    } catch (e) {
      if ((e as Error).name === 'AbortError') throw e;
      throw new ConnectionRequiredError();
    }
    return this.parse<T>(res);
  }

  /**
   * A server-data change (R-MUT-1…4): carries an Idempotency-Key, is never applied
   * optimistically, and is never replayed later. Offline → ConnectionRequiredError
   * (nothing sent). Sent but no answer → UnknownResultError (request ID kept).
   */
  async mutate<T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown, opts: MutateOptions = {}): Promise<T> {
    if (this.offline()) throw new ConnectionRequiredError();
    const requestId = opts.requestId ?? newRequestId();
    let res: Response;
    try {
      res = await this.fetch(this.opts.baseUrl + path, {
        method,
        headers: this.headers({ 'Content-Type': 'application/json', 'Idempotency-Key': requestId }),
        credentials: 'include',
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      this.outstanding.set(requestId, { requestId, method, path, at: Date.now() });
      throw new UnknownResultError(requestId);
    }
    this.outstanding.delete(requestId);
    return this.parse<T>(res);
  }

  /** Asks the server what happened to a request with an unknown result. */
  async mutationStatus(requestId: string): Promise<MutationStatus> {
    const s = await this.get<MutationStatus>(`/me/mutations/${encodeURIComponent(requestId)}`);
    this.outstanding.delete(requestId);
    return s;
  }

  /** Raw fetch with the client headers, for streams (SSE). */
  stream(path: string, signal: AbortSignal): Promise<Response> {
    return this.fetch(this.opts.baseUrl + path, { headers: this.headers({ Accept: 'text/event-stream' }), credentials: 'include', signal, cache: 'no-store' });
  }
}
