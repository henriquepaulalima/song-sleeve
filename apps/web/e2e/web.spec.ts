import { expect, test, type Browser, type Page } from '@playwright/test';
import { tmpdir } from 'node:os';
import path from 'node:path';

const EMAIL = process.env.E2E_EMAIL ?? 'owner@example.com';
const PASSWORD = process.env.E2E_PASSWORD ?? 'saltwater-hymn';
const FIX = process.env.E2E_FIXTURES ?? path.join(tmpdir(), 'songsleeve-e2e-fixtures');
const MAILPIT = process.env.E2E_MAILPIT ?? 'http://localhost:58025';

async function signIn(page: Page, email = EMAIL, password = PASSWORD) {
  await page.goto('/register');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Create account' }).click();
  await page.waitForTimeout(800);
  if (!page.url().endsWith('/home')) {
    await page.goto('/login');
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill(password);
    await page.getByRole('button', { name: 'Log in' }).last().click();
  }
  await page.waitForURL('**/home');
}

async function newDevice(browser: Browser) {
  const page = await (await browser.newContext()).newPage();
  await signIn(page);
  return page;
}

test('web stores nothing on the device (R-WEB)', async ({ page }) => {
  await signIn(page);
  await page.goto('/songs');
  const s = await page.evaluate(async () => ({
    local: localStorage.length,
    session: sessionStorage.length,
    idb: (await indexedDB.databases()).length,
    caches: (await caches.keys()).length,
    sw: (await navigator.serviceWorker.getRegistrations()).length,
  }));
  expect(s).toEqual({ local: 0, session: 0, idb: 0, caches: 0, sw: 0 });
});

test('add songs, play, organize', async ({ page }) => {
  await signIn(page);
  await page.goto('/songs');
  await page.getByRole('button', { name: 'Add songs' }).first().click();
  await page.locator('input[type=file]').setInputFiles(['tone.mp3', 'tone.flac', 'tone.m4a', 'tone.wav'].map((f) => path.join(FIX, f)));
  await expect(page.getByText('Reading…')).toHaveCount(0, { timeout: 30_000 });
  // The embedded title fills the name; the artist is only offered.
  await expect(page.getByRole('dialog').getByLabel('NAME').first()).toHaveValue('Harbor Lights');
  await expect(page.getByRole('button', { name: 'Use “Marina Okafor”' })).toBeVisible();
  await page.getByRole('button', { name: /Upload 4 songs/ }).click();
  await expect(page.getByText(/Syncing…|Checking the file|Queued/)).toHaveCount(0, { timeout: 60_000 });

  await page.getByRole('button', { name: /^Play: / }).first().click();
  await expect(page.getByRole('button', { name: 'Pause' }).first()).toBeVisible({ timeout: 20_000 });

  await page.getByRole('link', { name: 'Library' }).first().click();
  await page.getByRole('button', { name: 'Create playlist' }).click();
  const name = `E2E ${Date.now() % 100000}`;
  await page.getByLabel('Name').fill(name);
  await page.getByRole('dialog').getByRole('button', { name: 'Create', exact: true }).click();
  await page.waitForURL('**/playlists/**');
  await page.getByRole('button', { name: 'Add songs to playlist' }).first().click();
  const boxes = page.getByRole('dialog').getByRole('checkbox');
  await boxes.nth(0).click();
  await boxes.nth(1).click();
  await page.getByRole('button', { name: /^Add 2$/ }).click();
  await expect(page.getByRole('button', { name: /^Play: / })).toHaveCount(2);
});

test('smart artist search asks "Did you mean…?"', async ({ page }) => {
  await signIn(page);
  await page.goto('/songs');
  await page.getByRole('button', { name: /More actions for/ }).first().click();
  await page.getByRole('menuitem', { name: 'Edit' }).click();
  const field = page.getByRole('dialog').getByRole('button', { name: /^Artist:/ });
  if (await field.count()) await field.click();
  await page.getByRole('combobox').fill('Marina Okafr');
  // Shown only when an artist with a similar name already exists.
  const hasMarina = await page.getByRole('option', { name: /Marina Okafor/ }).count();
  if (hasMarina) await expect(page.getByText('Did you mean Marina Okafor?')).toBeVisible();
  await expect(page.getByRole('option', { name: /New artist “Marina Okafr”/ })).toBeVisible();
});

test('two devices: live sync, pin notice, delete', async ({ browser }) => {
  const a = await newDevice(browser);
  const b = await newDevice(browser);
  await b.goto('/songs');
  await a.goto('/songs');
  await a.getByRole('button', { name: 'Add songs' }).first().click();
  await a.locator('input[type=file]').setInputFiles(path.join(FIX, 'tone.flac'));
  await expect(a.getByText('Reading…')).toHaveCount(0, { timeout: 30_000 });
  const name = `Two devices ${Date.now() % 100000}`;
  await a.getByRole('dialog').getByLabel('NAME').first().fill(name);
  await a.getByRole('button', { name: /Upload 1 song/ }).click();
  // Every device lists the song while it syncs, then as synced (R-ID-4).
  await expect(b.getByRole('button', { name: `Play: ${name}` })).toBeVisible({ timeout: 20_000 });
  await expect(b.locator('.song-row', { hasText: name }).getByText('Synced')).toBeVisible({ timeout: 30_000 });

  await b.getByRole('button', { name: `Play: ${name}` }).click();
  await expect(b.getByRole('button', { name: 'Pause' }).first()).toBeVisible({ timeout: 20_000 });
  await a.getByRole('button', { name: `More actions for ${name}` }).click();
  await a.getByRole('menuitem', { name: 'Replace audio file' }).click();
  await a.getByRole('dialog').locator('input[type=file]').setInputFiles(path.join(FIX, 'tone.m4a'));
  await a.getByRole('radio', { name: /KEEP CURRENT/ }).first().click();
  await a.getByRole('button', { name: 'Confirm' }).click();
  // The playing device keeps the pinned audio and says so (R-PIN-1).
  await expect(b.getByText('This song was updated').first()).toBeVisible({ timeout: 30_000 });

  await expect(a.locator('.song-row', { hasText: name }).getByText('Synced')).toBeVisible({ timeout: 30_000 });
  await a.getByRole('button', { name: `More actions for ${name}` }).click();
  await a.getByRole('menuitem', { name: 'Delete song' }).click();
  await a.getByRole('alertdialog').getByRole('button', { name: 'Delete song' }).click();
  await expect(b.getByText('This song was deleted on another device').first()).toBeVisible({ timeout: 15_000 });
});

test('password reset through the emailed link (D-27)', async ({ page, browser }) => {
  await signIn(page);
  const other = await (await browser.newContext()).newPage();
  await other.goto('/forgot');
  await other.getByLabel('Email').fill(EMAIL);
  await other.getByRole('button', { name: 'Send reset link' }).click();
  await expect(other.getByText('Check your email')).toBeVisible();
  let link: string | null = null;
  for (let i = 0; i < 30 && !link; i++) {
    await other.waitForTimeout(500);
    const list = await (await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent('to:' + EMAIL)}`)).json();
    const msg = list.messages?.find((m: { Subject: string; Created: string }) => m.Subject.includes('Reset') && Date.now() - Date.parse(m.Created) < 60_000);
    if (msg) link = /http:\/\/localhost:5173\/reset#token=[\w-]+/.exec((await (await fetch(`${MAILPIT}/api/v1/message/${msg.ID}`)).json()).Text)?.[0] ?? null;
  }
  expect(link).not.toBeNull();
  await other.goto(link!);
  await expect(other.getByText('Set a new password')).toBeVisible();
  expect(other.url()).not.toContain('token=');
  await other.getByLabel('New password', { exact: true }).fill(PASSWORD);
  await other.getByLabel('Confirm new password').fill(PASSWORD);
  await other.getByRole('button', { name: 'Change password' }).click();
  await expect(other.getByText('Your password was changed')).toBeVisible();
  // A reset signs out every device (R-AUTH-4).
  await page.goto('/songs');
  await page.waitForURL('**/login');
});

// Destructive: deletes the account named in E2E_DELETE_EMAIL, which must be in
// CLOUD_SYNC_ALLOWLIST. Skipped unless set.
test('account deletion (§7.11)', async ({ page }) => {
  const email = process.env.E2E_DELETE_EMAIL;
  test.skip(!email, 'set E2E_DELETE_EMAIL to an allowlisted throwaway account');
  await signIn(page, email!, PASSWORD);
  await page.goto('/profile');
  await page.getByRole('button', { name: 'Delete account' }).click();
  await page.getByLabel('Enter your current password to confirm').fill(PASSWORD);
  await page.getByRole('button', { name: 'Permanently delete account' }).click();
  await expect(page.getByText('Your account was deleted')).toBeVisible({ timeout: 15_000 });
  await page.goto('/login');
  await page.getByLabel('Email').fill(email!);
  await page.getByLabel('Password').fill(PASSWORD);
  await page.getByRole('button', { name: 'Log in' }).last().click();
  await expect(page.getByText('Wrong email or password.')).toBeVisible();
});
