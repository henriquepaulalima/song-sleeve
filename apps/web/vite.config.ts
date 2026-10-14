import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The web reaches the API through its own origin at /api: a Vercel rewrite in
// production (§10.2), this proxy locally. The session cookie stays first-party.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    headers: { 'X-Robots-Tag': 'noindex' },
    proxy: {
      '/api': { target: process.env.SONG_SLEEVE_API ?? 'http://localhost:8088', changeOrigin: false },
    },
  },
  preview: { port: 5173, headers: { 'X-Robots-Tag': 'noindex' } },
  worker: { format: 'es' },
  define: {
    __APP_VERSION__: JSON.stringify(process.env.npm_package_version ?? '0.1.0'),
  },
});
