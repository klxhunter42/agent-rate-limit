import { test, expect } from '@playwright/test';

test('profiles page - wait for load and screenshot', async ({ page }) => {
  await page.goto('/profiles');
  await page.waitForResponse('**/v1/profiles', { timeout: 10000 }).catch(() => {});
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/tmp/profiles-page2.png', fullPage: true });
});

test('API - check profiles directly', async ({ request }) => {
  const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173';
  const resp = await request.get(`${baseURL}/v1/profiles`);
  const data = await resp.json();
  console.log('Profiles API response:', JSON.stringify(data, null, 2));
});

test('API - check account usage endpoint', async ({ request }) => {
  const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173';
  const resp = await request.get(`${baseURL}/v1/usage/accounts`);
  console.log('Account usage status:', resp.status());
  const data = await resp.json();
  console.log('Account usage:', JSON.stringify(data, null, 2));
  expect(resp.status()).toBe(200);
});
