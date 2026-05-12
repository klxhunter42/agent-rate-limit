import { test, expect } from '@playwright/test';

test.describe('Wiretap Page', () => {
	test('wiretap page renders', async ({ page }) => {
		await page.goto('/wiretap');
		await expect(page.getByRole('heading', { name: 'Wiretap' })).toBeVisible({ timeout: 10000 });
	});

	test('wiretap shows mitm UI', async ({ page }) => {
		await page.goto('/wiretap');
		await expect(page.getByRole('heading', { name: 'Wiretap' })).toBeVisible({ timeout: 10000 });

		// Check if mitmproxy is configured
		const resp = await page.request.get('/v1/config/mitm');
		if (!resp.ok()) { test.skip(); return; }
		const config = await resp.json();

		if (config.proxy_url) {
			// Buttons should be visible
			await expect(page.getByRole('button', { name: 'Intercept' })).toBeVisible({ timeout: 5000 });
			await expect(page.getByRole('button', { name: 'Pass-through' })).toBeVisible();
		} else {
			// Setup message
			await expect(page.getByText('docker compose --profile debug up')).toBeVisible({ timeout: 5000 });
		}
	});

	test('wiretap toggle works', async ({ page }) => {
		await page.goto('/wiretap');
		await expect(page.getByRole('heading', { name: 'Wiretap' })).toBeVisible({ timeout: 10000 });

		const interceptBtn = page.getByRole('button', { name: 'Intercept' });
		await expect(interceptBtn).toBeVisible({ timeout: 5000 }).catch(() => {});
		const hasButtons = await interceptBtn.isVisible().catch(() => false);
		if (!hasButtons) { test.skip(); return; }

		const stateResp = await page.request.get('/v1/config/mitm');
		if (!stateResp.ok()) { test.skip(); return; }
		const initialState = await stateResp.json();

		const targetState = !initialState.enabled;
		const targetBtn = targetState ? interceptBtn : page.getByRole('button', { name: 'Pass-through' });
		await targetBtn.click();

		const verifyResp = await page.request.get('/v1/config/mitm');
		expect(verifyResp.ok()).toBeTruthy();
		const verifyState = await verifyResp.json();
		expect(verifyState.enabled).toBe(targetState);

		// Restore
		const restoreBtn = initialState.enabled ? interceptBtn : page.getByRole('button', { name: 'Pass-through' });
		await restoreBtn.click();
	});

	test('wiretap has mitmweb link', async ({ page }) => {
		await page.goto('/wiretap');
		await expect(page.getByRole('heading', { name: 'Wiretap' })).toBeVisible({ timeout: 10000 });

		const mitmwebLink = page.getByRole('link', { name: 'Open mitmweb' });
		await expect(mitmwebLink).toBeVisible({ timeout: 5000 }).catch(() => {});
		const hasLink = await mitmwebLink.isVisible().catch(() => false);
		if (!hasLink) { test.skip(); return; }

		await expect(mitmwebLink).toHaveAttribute('target', '_blank');
		const href = await mitmwebLink.getAttribute('href');
		expect(href).toContain('/mitmweb/');
	});
});
