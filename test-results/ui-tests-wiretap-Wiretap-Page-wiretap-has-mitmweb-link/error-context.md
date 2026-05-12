# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: ui/tests/wiretap.spec.ts >> Wiretap Page >> wiretap has mitmweb link
- Location: ui/tests/wiretap.spec.ts:61:2

# Error details

```
Error: page.goto: Protocol error (Page.navigate): Cannot navigate to invalid URL
Call log:
  - navigating to "/wiretap", waiting until "load"

```

# Test source

```ts
  1  | import { test, expect } from '@playwright/test';
  2  | 
  3  | test.describe('Wiretap Page', () => {
  4  | 	test('wiretap page renders', async ({ page }) => {
  5  | 		await page.goto('/wiretap');
  6  | 		await expect(page.getByRole('heading', { name: 'Wiretap' })).toBeVisible({ timeout: 10000 });
  7  | 	});
  8  | 
  9  | 	test('wiretap shows intercept/pass-through buttons when mitm configured', async ({ page }) => {
  10 | 		await page.goto('/wiretap');
  11 | 		const heading = page.getByRole('heading', { name: 'Wiretap' });
  12 | 		await expect(heading).toBeVisible({ timeout: 10000 });
  13 | 
  14 | 		const interceptBtn = page.getByRole('button', { name: 'Intercept' });
  15 | 		const passthroughBtn = page.getByRole('button', { name: 'Pass-through' });
  16 | 
  17 | 		const hasButtons = await interceptBtn.isVisible().catch(() => false);
  18 | 		if (!hasButtons) {
  19 | 			await expect(page.getByText('docker compose --profile debug up')).toBeVisible();
  20 | 			return;
  21 | 		}
  22 | 
  23 | 		await expect(interceptBtn).toBeVisible();
  24 | 		await expect(passthroughBtn).toBeVisible();
  25 | 	});
  26 | 
  27 | 	test('wiretap toggle works', async ({ page }) => {
  28 | 		await page.goto('/wiretap');
  29 | 		const heading = page.getByRole('heading', { name: 'Wiretap' });
  30 | 		await expect(heading).toBeVisible({ timeout: 10000 });
  31 | 
  32 | 		const interceptBtn = page.getByRole('button', { name: 'Intercept' });
  33 | 		const passthroughBtn = page.getByRole('button', { name: 'Pass-through' });
  34 | 
  35 | 		const hasButtons = await interceptBtn.isVisible().catch(() => false);
  36 | 		if (!hasButtons) {
  37 | 			test.skip();
  38 | 			return;
  39 | 		}
  40 | 
  41 | 		const stateResp = await page.request.get('/v1/config/mitm');
  42 | 		if (!stateResp.ok()) {
  43 | 			test.skip();
  44 | 			return;
  45 | 		}
  46 | 		const initialState = await stateResp.json();
  47 | 
  48 | 		const targetState = !initialState.enabled;
  49 | 		const targetBtn = targetState ? interceptBtn : passthroughBtn;
  50 | 		await targetBtn.click();
  51 | 
  52 | 		const verifyResp = await page.request.get('/v1/config/mitm');
  53 | 		expect(verifyResp.ok()).toBeTruthy();
  54 | 		const verifyState = await verifyResp.json();
  55 | 		expect(verifyState.enabled).toBe(targetState);
  56 | 
  57 | 		const restoreBtn = initialState.enabled ? interceptBtn : passthroughBtn;
  58 | 		await restoreBtn.click();
  59 | 	});
  60 | 
  61 | 	test('wiretap has mitmweb link', async ({ page }) => {
> 62 | 		await page.goto('/wiretap');
     |              ^ Error: page.goto: Protocol error (Page.navigate): Cannot navigate to invalid URL
  63 | 		const heading = page.getByRole('heading', { name: 'Wiretap' });
  64 | 		await expect(heading).toBeVisible({ timeout: 10000 });
  65 | 
  66 | 		const mitmwebLink = page.getByRole('link', { name: 'Open mitmweb' });
  67 | 		const hasLink = await mitmwebLink.isVisible().catch(() => false);
  68 | 		if (!hasLink) {
  69 | 			test.skip();
  70 | 			return;
  71 | 		}
  72 | 		await expect(mitmwebLink).toHaveAttribute('target', '_blank');
  73 | 		await expect(mitmwebLink).toHaveAttribute('href', 'http://localhost:8081');
  74 | 	});
  75 | });
  76 | 
```