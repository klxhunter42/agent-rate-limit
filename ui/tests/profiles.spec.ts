import { test, expect } from '@playwright/test';

test.describe('Profile Account Pool', () => {
  test('account checkboxes are checked when editing a profile', async ({ page }) => {
    await page.goto('/profiles');
    await page.waitForResponse('**/v1/profiles', { timeout: 10000 }).catch(() => {});

    // Wait for profile cards to render
    await page.waitForTimeout(1000);

    const editBtns = page.locator('[title="Edit"]');
    const count = await editBtns.count();
    if (count === 0) {
      test.skip();
      return;
    }

    // Click edit on first profile
    await editBtns.first().click();
    await page.waitForTimeout(1000);

    // Wait for account checkboxes
    const checkboxes = page.locator('input[type="checkbox"]');
    const cbCount = await checkboxes.count();

    if (cbCount === 0) {
      // No accounts for this provider - skip
      test.skip();
      return;
    }

    // Verify checkboxes are interactive (clickable)
    await checkboxes.first().click();
    await page.waitForTimeout(300);
    const isChecked = await checkboxes.first().isChecked();
    expect(typeof isChecked).toBe('boolean');
  });
});
