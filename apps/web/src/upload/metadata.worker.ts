/// <reference lib="webworker" />
/**
 * Client metadata extraction off the UI thread (F-12): duration, real codec, tags
 * and SHA-256 over the complete file (R-ID-6). Any parse failure is treated as "no
 * embedded value"; the codec decides the format, never the extension (D-8).
 */
import { createSHA256 } from 'hash-wasm';
import { parseBlob } from 'music-metadata';

export interface ExtractRequest {
  id: number;
  file: File;
}

export interface ExtractResult {
  id: number;
  format: 'mp3' | 'aac' | 'flac' | 'wav' | null;
  codec: string | null;
  durationMs: number | null;
  title: string | null;
  artist: string | null;
  sha256: string | null;
  error?: string;
}

function formatFrom(codec: string | undefined, container: string | undefined): ExtractResult['format'] {
  const c = (codec ?? '').toUpperCase();
  const box = (container ?? '').toUpperCase();
  if (c.includes('LAYER 3') || c === 'MP3') return 'mp3';
  if (c.includes('AAC')) return 'aac';
  if (c === 'FLAC') return 'flac';
  if (box.includes('WAVE') && (c.includes('PCM') || c === '' || c.includes('FLOAT'))) return 'wav';
  return null;
}

async function sha256(file: File): Promise<string> {
  const h = await createSHA256();
  h.init();
  const reader = file.stream().getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    h.update(value);
  }
  return h.digest('hex');
}

self.onmessage = async (e: MessageEvent<ExtractRequest>) => {
  const { id, file } = e.data;
  const out: ExtractResult = { id, format: null, codec: null, durationMs: null, title: null, artist: null, sha256: null };
  try {
    const meta = await parseBlob(file, { duration: true, skipCovers: true });
    out.codec = meta.format.codec ?? null;
    out.format = formatFrom(meta.format.codec, meta.format.container);
    const d = meta.format.duration;
    out.durationMs = d && Number.isFinite(d) && d > 0 ? Math.round(d * 1000) : null;
    out.title = meta.common.title?.trim() || null;
    out.artist = meta.common.artist?.trim() || null;
  } catch {
    // No embedded values; the format stays unknown.
  }
  try {
    if (out.format) out.sha256 = await sha256(file);
  } catch (err) {
    out.error = String(err);
  }
  (self as unknown as Worker).postMessage(out);
};
