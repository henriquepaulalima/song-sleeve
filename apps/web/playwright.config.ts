import { defineConfig } from '@playwright/test';

/**
 * Browser tests for the web app. They expect the local stack to be running:
 * `make infra`, `make api` and `make web`. The account in E2E_EMAIL must be in
 * CLOUD_SYNC_ALLOWLIST (owner@example.com by default).
 */
export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  fullyParallel: false,
  workers: 1,
  globalSetup: './e2e/global-setup.ts',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
    viewport: { width: 1440, height: 950 },
    launchOptions: { args: ['--autoplay-policy=no-user-gesture-required'] },
    trace: 'retain-on-failure',
  },
});
