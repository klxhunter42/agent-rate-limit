import { test, expect } from '@playwright/test';

const GATEWAY = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173';
const API = process.env.PLAYWRIGHT_API_URL || 'http://localhost:8080';

test.describe('Delete Profile', () => {
  test('delete profile via UI', async ({ page, request }) => {
    const name = 'test-delete-' + Date.now();

    await test.step('create profile via API', async () => {
      const resp = await request.post(`${API}/v1/profiles`, {
        data: { name, target: 'claude-oauth' },
      });
      expect(resp.status()).toBe(201);
    });

    await test.step('open profiles page', async () => {
      await page.goto(GATEWAY + '/profiles');
      await page.waitForResponse('**/v1/profiles', { timeout: 10000 });
      await page.waitForTimeout(1000);
    });

    await test.step('click delete and confirm', async () => {
      const deleteBtn = page.locator('button[title="Delete"]').first();
      await expect(deleteBtn).toBeVisible({ timeout: 5000 });
      await deleteBtn.click();

      const confirmBtn = page.locator('button:has-text("Delete")').last();
      await expect(confirmBtn).toBeVisible({ timeout: 3000 });
      await confirmBtn.click();
    });

    await test.step('verify profile is deleted', async () => {
      const deleteResp = await page.waitForResponse(
        (r) => r.url().includes('/v1/profiles/') && r.request().method() === 'DELETE',
        { timeout: 10000 }
      );
      expect(deleteResp.status()).toBe(200);

      await page.waitForTimeout(500);
      const remaining = await page.locator('.font-mono.font-semibold').allTextContents();
      expect(remaining).not.toContain(name);
    });
  });
});
