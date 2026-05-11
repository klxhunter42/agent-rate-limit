import { test, expect } from '@playwright/test';

const BASE = 'http://localhost:9000';

test.describe.configure({ mode: 'serial' });

test('API - custom provider in /v1/providers', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/providers`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  const custom = data.filter((p: { id: string }) => p.id.startsWith('custom-'));
  console.log('Custom providers:', JSON.stringify(custom, null, 2));
  expect(custom.length).toBeGreaterThanOrEqual(1);
  expect(custom.some((p: { id: string }) => p.id === 'custom-09ab6a')).toBeTruthy();
});

test('API - custom provider in recommended-models', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/profiles/recommended-models`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  const models = data.models ?? data;
  console.log('Recommended models keys:', Object.keys(models));
  expect(models).toHaveProperty('custom-09ab6a');
});

test('API - custom provider recommended-models by target', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/profiles/recommended-models?target=custom-09ab6a`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  console.log('Target models:', JSON.stringify(data, null, 2));
  expect(data.target ?? data.provider).toBe('custom-09ab6a');
  expect((data.models ?? []).length).toBeGreaterThanOrEqual(1);
});

test('API - accounts show custom provider', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/auth/accounts`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  const accounts = data.accounts ?? data;
  const custom = accounts.filter((a: { provider: string }) => a.provider?.startsWith('custom-'));
  console.log('Custom provider accounts:', JSON.stringify(custom, null, 2));
  expect(custom.length).toBeGreaterThanOrEqual(1);
});

test('UI - custom provider in profile dropdown', async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  page.setDefaultTimeout(15000);

  // Login via API first to get session cookie
  const loginResp = await page.request.post(`${BASE}/v1/auth/login`, {
    data: { password: 'klxhunter' },
  });
  console.log('Login status:', loginResp.status());

  // Navigate to profiles
  await page.goto(`${BASE}/profiles`, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(3000);

  // Take debug screenshot before clicking
  await page.screenshot({ path: '/tmp/custom-provider-before-click.png', fullPage: true });

  // Click the "New" button to open the create profile form (contains Plus icon + "New" text)
  const newBtn = page.getByRole('button', { name: /New/ }).first();
  if (!(await newBtn.isVisible({ timeout: 5000 }).catch(() => false))) {
    // Try alternative selector
    const altBtn = page.locator('button:has-text("New")').first();
    if (await altBtn.isVisible({ timeout: 3000 }).catch(() => false)) {
      await altBtn.click();
    } else {
      const body = await page.locator('body').textContent();
      console.log('Page body (first 500 chars):', body?.slice(0, 500));
      throw new Error('New button not found');
    }
  } else {
    await newBtn.click();
  }

  // Wait for providers to load (select options appear)
  await page.waitForFunction(() => {
    const sel = document.querySelector('select');
    if (!sel) return false;
    const opts = Array.from(sel.options).map(o => o.textContent || '');
    return opts.length > 1 && !opts.includes('Loading...');
  }, { timeout: 10000 });

  await page.screenshot({ path: '/tmp/custom-provider-dropdown.png', fullPage: true });

  // Check all selects for Meow Gateway
  const selects = page.locator('select');
  const count = await selects.count();
  console.log(`Found ${count} select elements`);

  let found = false;
  for (let i = 0; i < count; i++) {
    const options = await selects.nth(i).locator('option').allTextContents();
    console.log(`Select ${i} options:`, options);
    if (options.some((o: string) => o.includes('Meow Gateway') || o.includes('custom-09ab6a'))) {
      found = true;
      break;
    }
  }

  expect(found).toBeTruthy();

  await context.close();
});
