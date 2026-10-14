import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

/** Generates small audio fixtures with ffmpeg, one per supported format. */
export default function globalSetup() {
  const dir = path.join(tmpdir(), 'songsleeve-e2e-fixtures');
  mkdirSync(dir, { recursive: true });
  const make = (file: string, args: string[]) => execFileSync('ffmpeg', ['-loglevel', 'error', '-y', ...args, path.join(dir, file)]);
  make('tone.mp3', ['-f', 'lavfi', '-i', 'sine=frequency=440:duration=20', '-metadata', 'title=Harbor Lights', '-metadata', 'artist=Marina Okafor', '-b:a', '128k']);
  make('tone.flac', ['-f', 'lavfi', '-i', 'sine=frequency=660:duration=25', '-c:a', 'flac']);
  make('tone.m4a', ['-f', 'lavfi', '-i', 'sine=frequency=550:duration=12', '-c:a', 'aac', '-b:a', '96k']);
  make('tone.wav', ['-f', 'lavfi', '-i', 'sine=frequency=330:duration=8']);
  process.env.E2E_FIXTURES = dir;
  // The tests sign in many times; start from fresh rate-limit windows on the
  // local development database (never run against anything else).
  if (!process.env.E2E_BASE_URL) {
    try {
      execFileSync('docker', ['compose', 'exec', '-T', 'postgres', 'psql', '-U', 'songsleeve', '-c', 'DELETE FROM rate_limit_buckets'], { cwd: path.resolve(process.cwd(), '../..'), stdio: 'ignore' });
    } catch {
      // Not the docker-compose stack: rate limits stay as they are.
    }
  }
}
