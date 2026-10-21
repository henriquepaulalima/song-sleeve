import { describe, expect, it, vi } from 'vitest';
import { PlayerController, sourceHref, type EngineEvents, type PlayableTrack, type PlayerEngine } from './controller';

class FakeEngine implements PlayerEngine {
  events: EngineEvents | null = null;
  loads: string[] = [];
  playing = false;
  async load(source: { trackId: string; contentId: string }, events: EngineEvents) {
    this.events = events;
    this.loads.push(`${source.trackId}@${source.contentId}`);
  }
  async play() {
    this.playing = true;
    this.events?.onPlaying(true);
  }
  pause() {
    this.playing = false;
    this.events?.onPlaying(false);
  }
  seek = vi.fn();
  unload = vi.fn();
}

function setup(tracks: Record<string, Partial<PlayableTrack>>) {
  const db = new Map<string, PlayableTrack>(
    Object.entries(tracks).map(([id, t]) => [id, { id, name: id, artist: null, durationMs: 200_000, contentId: `${id}-c1`, playable: true, ...t }]),
  );
  const engine = new FakeEngine();
  const ctl = new PlayerController(engine, (id) => db.get(id) ?? null);
  return { db, engine, ctl };
}

const src = { kind: 'songs' as const, label: 'Songs' };

describe('PlayerController', () => {
  it('snapshots the queue and skips unplayable songs (R-PLAY-3)', async () => {
    const { ctl, engine } = setup({ a: {}, b: { playable: false }, c: {} });
    await ctl.playList(['a', 'b', 'c'], 'a', src);
    await ctl.next();
    expect(engine.loads).toEqual(['a@a-c1', 'c@c-c1']);
    expect(ctl.hasNext()).toBe(false);
  });

  it('restarts after 3 s, otherwise goes back', async () => {
    const { ctl, engine } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'b', src);
    engine.events!.onTime(10);
    await ctl.previous();
    expect(engine.seek).toHaveBeenCalledWith(0);
    expect(ctl.getState().current?.id).toBe('b');
    engine.events!.onTime(1);
    await ctl.previous();
    expect(ctl.getState().current?.id).toBe('a');
  });

  it('turns repeat-one off on every track change (R-PLAY-4)', async () => {
    const { ctl } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'a', src);
    ctl.toggleRepeat();
    expect(ctl.getState().repeatOne).toBe(true);
    await ctl.next();
    expect(ctl.getState().repeatOne).toBe(false);
  });

  it('repeats the current song on end while repeat-one is on', async () => {
    const { ctl, engine } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'a', src);
    ctl.toggleRepeat();
    engine.events!.onEnded();
    await Promise.resolve();
    expect(engine.seek).toHaveBeenCalledWith(0);
    expect(ctl.getState().current?.id).toBe('a');
  });

  it('keeps the current song in place on shuffle and restores order on unshuffle', async () => {
    const ids = ['a', 'b', 'c', 'd', 'e'];
    const { ctl } = setup(Object.fromEntries(ids.map((id) => [id, {}])));
    await ctl.playList(ids, 'c', src);
    ctl.setShuffle(true);
    expect(ctl.getState().queue[ctl.getState().index]).toBe('c');
    expect([...ctl.getState().queue].sort()).toEqual(ids);
    ctl.setShuffle(false);
    expect(ctl.getState().queue).toEqual(ids);
    expect(ctl.getState().index).toBe(2);
  });

  it('pins the playing resource across a replacement (R-PIN-1…3)', async () => {
    const { ctl, db, engine } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'a', src);
    db.set('a', { ...db.get('a')!, contentId: 'a-c2' });
    ctl.onTrackUpdated('a');
    expect(ctl.getState().notice).toBe('updated');
    expect(ctl.getState().current?.contentId).toBe('a-c1');
    // Repeat processes the boundary first: the new content loads.
    ctl.toggleRepeat();
    engine.events!.onEnded();
    await Promise.resolve();
    await Promise.resolve();
    expect(engine.loads.at(-1)).toBe('a@a-c2');
    expect(ctl.getState().notice).toBeNull();
  });

  it('keeps playing a song deleted elsewhere until it ends, then skips it', async () => {
    const { ctl, db, engine } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'a', src);
    db.delete('a');
    ctl.onTrackDeleted('a', false);
    expect(ctl.getState().notice).toBe('removed');
    expect(ctl.getState().status).toBe('playing');
    engine.events!.onEnded();
    await Promise.resolve();
    expect(ctl.getState().current?.id).toBe('b');
  });

  it('stops at once when the current song is deleted on this device (R-PIN-5)', async () => {
    const { ctl, engine } = setup({ a: {}, b: {} });
    await ctl.playList(['a', 'b'], 'a', src);
    ctl.onTrackDeleted('a', true);
    expect(ctl.getState().status).toBe('empty');
    expect(engine.unload).toHaveBeenCalled();
  });

  it('restores a snapshot paused at the saved position, without missing songs (R-PLAY-5…8)', async () => {
    const a = setup({ a: {}, b: {}, c: {} });
    await a.ctl.playList(['a', 'b', 'c'], 'b', { kind: 'playlist', id: 'p1', label: 'Mix' });
    a.engine.events!.onTime(42.7);
    a.ctl.toggleRepeat();
    const snap = a.ctl.snapshot()!;
    expect(snap).toMatchObject({ trackId: 'b', positionSec: 42, repeatOne: true, source: { kind: 'playlist', id: 'p1' } });

    const b = setup({ b: {}, c: {} });
    expect(await b.ctl.restore(snap)).toBe(true);
    const st = b.ctl.getState();
    expect(st.status).toBe('paused');
    expect(st.current?.id).toBe('b');
    expect(st.queue).toEqual(['b', 'c']);
    expect(st.repeatOne).toBe(true);
    expect(b.engine.playing).toBe(false);
    expect(b.engine.seek).toHaveBeenCalledWith(42);
  });

  it('does not restore over playback or when the saved song is gone', async () => {
    const { ctl } = setup({ a: {} });
    const snap = { v: 1 as const, trackId: 'x', queue: ['x'], original: ['x'], source: null, shuffle: false, repeatOne: false, positionSec: 3 };
    expect(await ctl.restore(snap)).toBe(false);
    await ctl.playList(['a'], 'a', src);
    expect(await ctl.restore({ ...snap, trackId: 'a', queue: ['a'] })).toBe(false);
    expect(ctl.snapshot()?.trackId).toBe('a');
  });

  it('links every queue source to its page', () => {
    expect(sourceHref({ kind: 'songs', label: '' })).toBe('/songs');
    expect(sourceHref({ kind: 'home', label: '' })).toBe('/home');
    expect(sourceHref({ kind: 'library', label: '' })).toBe('/library');
    expect(sourceHref({ kind: 'group', id: 'g', label: '' })).toBe('/groups/g');
    expect(sourceHref(null)).toBe('/songs');
  });
});
