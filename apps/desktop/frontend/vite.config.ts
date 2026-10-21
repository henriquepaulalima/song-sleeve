import { fileURLToPath } from 'node:url';
import path from 'node:path';
import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

const here = path.dirname(fileURLToPath(import.meta.url));
const web = path.resolve(here, '../../web/src');
const own = path.resolve(here, 'src');

/**
 * The desktop UI is the web app's screens with a few modules swapped for desktop
 * ones (§4.3): the services wiring, the platform hooks and the Add songs dialog.
 * Everything else is shared source, not a copy.
 */
const overrides: Record<string, string> = {
  [path.join(web, 'app/services.ts')]: path.join(own, 'services.ts'),
  [path.join(web, 'app/platform.ts')]: path.join(own, 'platform.ts'),
  [path.join(web, 'dialogs/AddSongsDialog.tsx')]: path.join(own, 'AddSongsDialog.tsx'),
};

function desktopOverrides(): Plugin {
  return {
    name: 'song-sleeve-desktop-overrides',
    enforce: 'pre',
    async resolveId(source, importer, opts) {
      if (!importer || !source.startsWith('.')) return null;
      // Desktop modules may import the web originals they wrap.
      if (importer.startsWith(own)) return null;
      const r = await this.resolve(source, importer, { ...opts, skipSelf: true });
      if (r && overrides[r.id]) return overrides[r.id];
      return null;
    },
  };
}

export default defineConfig({
  plugins: [desktopOverrides(), react()],
  resolve: {
    alias: { '@web': web },
    dedupe: ['react', 'react-dom', 'react-router', '@song-sleeve/core'],
  },
  worker: { format: 'es' },
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  define: {
    __APP_VERSION__: JSON.stringify(process.env.SONG_SLEEVE_VERSION ?? process.env.npm_package_version ?? '0.1.0'),
  },
});
