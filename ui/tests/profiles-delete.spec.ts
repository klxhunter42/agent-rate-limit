import { test, expect } from '@playwright/test';

test.describe('Delete Profile', () => {
  test('delete profile via UI', async ({ page, request }) => {
    const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173';
    const name = 'test-delete-' + Date.now();

    await test.step('create profile via API', async () => {
      let resp = await request.post(`${baseURL}/v1/profiles`, {
        data: { name, target: 'claude-oauth' },
      });
      if (resp.status() !== 201) {
        await new Promise((r) => setTimeout(r, 1000));
        resp = await request.post(`${baseURL}/v1/profiles`, {
          data: { name, target: 'claude-oauth' },
        });
      }
      expect(resp.status()).toBe(201);
    });

    await test.step('open profiles page', async () => {
      await page.goto(baseURL + '/profiles');
      await page.waitForResponse('**/v1/profiles', { timeout: 10000 });
      await page.waitForTimeout(1000);
    });

    await test.step('click delete and confirm', async () => {
      // Find the profile name span, navigate to its sibling Delete button
      // DOM: div.flex > div.gap-3 > span(name) | div.gap-1 > button[title=Delete]
      const nameSpan = page.locator(`span.font-mono.font-semibold:text-is("${name}")`);
      await expect(nameSpan).toBeVisible({ timeout: 5000 });
      const deleteBtn = nameSpan.locator('xpath=../following-sibling::div/button[@title="Delete"]');
      await expect(deleteBtn).toBeVisible({ timeout: 3000 });
      await deleteBtn.click();

      // Wait for confirmation dialog
      const dialog = page.locator('[role="dialog"]');
      await expect(dialog).toBeVisible({ timeout: 3000 });
      const confirmBtn = dialog.locator('button:has-text("Delete")');
      await expect(confirmBtn).toBeVisible({ timeout: 3000 });

      // Listen for delete API call before clicking confirm
      const deletePromise = page.waitForResponse(
        (r) => r.url().includes('/v1/profiles/delete') && r.request().method() === 'POST',
        { timeout: 10000 }
      );
      await confirmBtn.click();
      const deleteResp = await deletePromise;
      expect(deleteResp.status()).toBe(200);
    });

    await test.step('verify profile is removed from list', async () => {
      await page.reload();
      await page.waitForResponse('**/v1/profiles', { timeout: 10000 });
      await page.waitForTimeout(500);
      const remaining = await page.locator('.font-mono.font-semibold').allTextContents();
      expect(remaining).not.toContain(name);
    });
  });
});
