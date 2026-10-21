/**
 * The webview's link to the Go shell: its HTTP API at /local (served by the Wails
 * asset server) and the events it emits through the Wails runtime.
 */
import { ApiError, type ErrorCode, type Me } from '@song-sleeve/core';

declare global {
  interface Window {
    runtime?: {
      EventsOn(name: string, cb: (data: unknown) => void): () => void;
      WindowShow?(): void;
    };
  }
}

export interface ShellState {
  signed_in: boolean;
  me?: Me;
  online: boolean;
  version: string;
  media_url: string;
  keyring: boolean;
  folder?: { root: string; account: string };
  data_dir: string;
}

export async function local<T>(method: 'GET' | 'POST' | 'PUT', path: string, body?: unknown): Promise<T> {
  const res = await fetch('/local' + path, {
    method,
    keepalive: true,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e = data?.error ?? {};
    const err = new ApiError((e.code ?? 'internal') as ErrorCode, e.message ?? res.statusText, res.status, data);
    throw err;
  }
  return data as T;
}

export function onShell(name: string, cb: (data: unknown) => void): () => void {
  return window.runtime?.EventsOn(name, cb) ?? (() => undefined);
}

/**
 * fetch for the API client: the shell answers 504 + X-Song-Sleeve-Unknown when a
 * request was sent but got no answer, which the client must see as a network
 * failure so it keeps the request ID (R-MUT-4).
 */
export const shellFetch: typeof fetch = async (input, init) => {
  const res = await fetch(input, init);
  if (res.headers.get('X-Song-Sleeve-Unknown')) throw new TypeError('Failed to fetch');
  return res;
};
