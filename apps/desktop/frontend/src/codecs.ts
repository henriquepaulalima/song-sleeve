/**
 * Which D-8 formats this system can play (§9.4). On Linux WebKitGTK plays through
 * GStreamer, and canPlayType answers from the plugins installed, so a missing
 * decoder shows here before any song fails.
 */
import type { Track } from '@song-sleeve/core';

export type AudioFormat = Track['format'];

export const FORMATS: AudioFormat[] = ['mp3', 'aac', 'flac', 'wav'];

const TYPES: Record<AudioFormat, string> = {
  mp3: 'audio/mpeg',
  aac: 'audio/mp4; codecs="mp4a.40.2"',
  flac: 'audio/flac',
  wav: 'audio/wav',
};

export const FORMAT_NAMES: Record<AudioFormat, string> = { mp3: 'MP3', aac: 'AAC', flac: 'FLAC', wav: 'WAV' };

/** Debian and Ubuntu packages that bring each decoder. */
const PACKAGES: Record<AudioFormat, string> = {
  mp3: 'gstreamer1.0-plugins-good',
  aac: 'gstreamer1.0-libav',
  flac: 'gstreamer1.0-plugins-good',
  wav: 'gstreamer1.0-plugins-good',
};

let missing: AudioFormat[] | null = null;

/** Formats with no decoder; the plugins are read once, at startup. */
export function missingFormats(): AudioFormat[] {
  if (!missing) {
    const audio = document.createElement('audio');
    missing = FORMATS.filter((f) => audio.canPlayType(TYPES[f]) === '');
  }
  return missing;
}

export function aptCommand(formats: AudioFormat[]): string {
  return `sudo apt install ${[...new Set(formats.map((f) => PACKAGES[f]))].join(' ')}`;
}

export function listFormats(formats: AudioFormat[], locale: string): string {
  return new Intl.ListFormat(locale, { type: 'conjunction' }).format(formats.map((f) => FORMAT_NAMES[f]));
}
