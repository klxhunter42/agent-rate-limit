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

    // Check that at least one checkbox is checked
    const states: boolean[] = [];
    for (let i = 0; i < cbCount; i++) {
      states.push(await checkboxes.nth(i).isChecked());
    }
    console.log('checkbox states:', states);

    expect(states.some(Boolean)).toBeTruthy();
  });
});
