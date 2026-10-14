import type { ExtractRequest, ExtractResult } from './metadata.worker';

export type { ExtractResult };

let worker: Worker | null = null;
let seq = 0;
const pending = new Map<number, (r: ExtractResult) => void>();

function getWorker(): Worker {
  if (!worker) {
    worker = new Worker(new URL('./metadata.worker.ts', import.meta.url), { type: 'module' });
    worker.onmessage = (e: MessageEvent<ExtractResult>) => {
      pending.get(e.data.id)?.(e.data);
      pending.delete(e.data.id);
    };
  }
  return worker;
}

/** Reads a file's facts in the worker, with a timeout; falls back to the audio element for duration. */
export async function extractFacts(file: File, timeoutMs = 120_000): Promise<ExtractResult> {
  const id = ++seq;
  const result = await new Promise<ExtractResult>((resolve) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      resolve({ id, format: null, codec: null, durationMs: null, title: null, artist: null, sha256: null, error: 'timeout' });
    }, timeoutMs);
    pending.set(id, (r) => {
      clearTimeout(timer);
      resolve(r);
    });
    getWorker().postMessage({ id, file } satisfies ExtractRequest);
  });
  if (result.format && result.durationMs == null) result.durationMs = await durationFromElement(file);
  return result;
}

/** The browser's own duration reading, used only when the tags have none. */
function durationFromElement(file: File): Promise<number | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const a = new Audio();
    const done = (v: number | null) => {
      URL.revokeObjectURL(url);
      a.removeAttribute('src');
      resolve(v);
    };
    a.preload = 'metadata';
    a.onloadedmetadata = () => done(Number.isFinite(a.duration) && a.duration > 0 ? Math.round(a.duration * 1000) : null);
    a.onerror = () => done(null);
    setTimeout(() => done(null), 15_000);
    a.src = url;
  });
}

/** The filename without its extension. */
export function baseName(file: File): string {
  const n = file.name;
  const i = n.lastIndexOf('.');
  return i > 0 ? n.slice(0, i) : n;
}

const AUDIO_EXT = /\.(mp3|m4a|aac|mp4|flac|wav|wave|ogg|opus|oga|wma|aif|aiff|alac)$/i;

/** Quick pre-filter: obvious non-audio files are reported as unsupported at once. */
export function looksLikeAudio(file: File): boolean {
  return file.type.startsWith('audio/') || AUDIO_EXT.test(file.name);
}
