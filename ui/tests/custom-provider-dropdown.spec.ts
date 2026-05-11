import { test, expect } from '@playwright/test';

const BASE = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:9000';

test('custom provider appears in profile target dropdown', async ({ page }) => {
  await page.goto('/profiles');
  await page.waitForTimeout(3000);

  // Find all target select dropdowns
  const selects = page.locator('select');
  const count = await selects.count();
  console.log(`Found ${count} select elements`);

  // Take screenshot for visual verification
  await page.screenshot({ path: '/tmp/custom-provider-dropdown.png', fullPage: true });

  // Check if any select contains Meow Gateway
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
});

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

test('API - accounts show custom provider', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/auth/accounts`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  const accounts = data.accounts ?? data;
  const custom = accounts.filter((a: { provider: string }) => a.provider?.startsWith('custom-'));
  console.log('Custom provider accounts:', JSON.stringify(custom, null, 2));
  expect(custom.length).toBeGreaterThanOrEqual(1);
});

test('API - custom provider recommended-models by target', async ({ request }) => {
  const resp = await request.get(`${BASE}/v1/profiles/recommended-models?target=custom-09ab6a`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  console.log('Target models:', JSON.stringify(data, null, 2));
  expect(data.target ?? data.provider).toBe('custom-09ab6a');
  expect((data.models ?? []).length).toBeGreaterThanOrEqual(1);
});
