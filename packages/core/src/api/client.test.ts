import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError, ConnectionRequiredError, UnknownResultError } from './client';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function client(opts: Partial<ConstructorParameters<typeof ApiClient>[0]> = {}) {
  return new ApiClient({ baseUrl: '/api', platform: 'web', version: '1.2.3', isOnline: () => true, ...opts });
}

afterEach(() => vi.unstubAllGlobals());

describe('ApiClient', () => {
  it('sends the client headers, CSRF token and an Idempotency-Key on changes (R-MUT-3, F-5)', async () => {
    const fetch = vi.fn().mockResolvedValue(json(200, { ok: true }));
    vi.stubGlobal('fetch', fetch);
    const c = client();
    c.setCsrfToken('csrf-1');
    await c.mutate('POST', '/me/playlists', { name: 'x' }, { requestId: 'req-1' });
    const [url, init] = fetch.mock.calls[0]!;
    const h = init.headers as Headers;
    expect(url).toBe('/api/me/playlists');
    expect(h.get('X-Client-Platform')).toBe('web');
    expect(h.get('X-Client-Version')).toBe('1.2.3');
    expect(h.get('X-CSRF-Token')).toBe('csrf-1');
    expect(h.get('Idempotency-Key')).toBe('req-1');
    expect(init.credentials).toBe('include');
  });

  it('uses a bearer token when one is configured', async () => {
    const fetch = vi.fn().mockResolvedValue(json(200, {}));
    vi.stubGlobal('fetch', fetch);
    await client({ platform: 'desktop', token: () => 'tok' }).get('/me');
    expect((fetch.mock.calls[0]![1].headers as Headers).get('Authorization')).toBe('Bearer tok');
  });

  it('maps the error envelope to ApiError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(409, { error: { code: 'revision_conflict', message: 'm', details: { current_revision: 4 } } })));
    const err = await client().mutate('PATCH', '/x', {}).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ code: 'revision_conflict', status: 409, details: { current_revision: 4 } });
  });

  it('is offline-aware: nothing is sent and the change is not saved (R-MUT-1)', async () => {
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    const err = await client({ isOnline: () => false }).mutate('POST', '/x', {}).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ConnectionRequiredError);
    expect((err as ConnectionRequiredError).code).toBe('connection_required');
    expect(fetch).not.toHaveBeenCalled();
  });

  it('keeps the request ID of a change whose result is unknown, and recovers it (R-MUT-4)', async () => {
    const fetch = vi.fn().mockRejectedValueOnce(new TypeError('network'));
    vi.stubGlobal('fetch', fetch);
    const c = client();
    const err = await c.mutate('DELETE', '/me/tracks/t', undefined, { requestId: 'lost-1' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(UnknownResultError);
    expect((err as UnknownResultError).requestId).toBe('lost-1');
    expect([...c.outstanding.keys()]).toEqual(['lost-1']);
    fetch.mockResolvedValueOnce(json(200, { status: 'committed', http_status: 200, result: {}, operation_id: null }));
    const s = await c.mutationStatus('lost-1');
    expect(s.status).toBe('committed');
    expect(fetch.mock.calls[1]![0]).toBe('/api/me/mutations/lost-1');
    expect(c.outstanding.size).toBe(0);
  });

  it('a failed read is connection_required, never an unknown result', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network')));
    const c = client();
    await expect(c.get('/me')).rejects.toBeInstanceOf(ConnectionRequiredError);
    expect(c.outstanding.size).toBe(0);
  });

  it('calls the unauthenticated and update-required hooks', async () => {
    const onUnauthenticated = vi.fn();
    const onUpdateRequired = vi.fn();
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(json(401, { error: { code: 'unauthenticated', message: '' } }))
      .mockResolvedValueOnce(json(426, { error: { code: 'client_update_required', message: '', details: { minimum_version: '2.0.0' } } }));
    vi.stubGlobal('fetch', fetch);
    const c = client({ onUnauthenticated, onUpdateRequired });
    await c.get('/me').catch(() => undefined);
    await c.get('/me').catch(() => undefined);
    expect(onUnauthenticated).toHaveBeenCalledOnce();
    expect(onUpdateRequired).toHaveBeenCalledWith('2.0.0');
  });

  it('handles 204 responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    await expect(client().post('/auth/logout')).resolves.toBeUndefined();
  });
});
