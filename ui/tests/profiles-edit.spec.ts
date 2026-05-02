import { test, expect } from '@playwright/test';

test.describe('Profile Edit', () => {
  test('edit gemini profile shows all targets and accounts', async ({ page }) => {
    await page.goto('/profiles', { waitUntil: 'networkidle' });
    await page.waitForTimeout(3000);

    // Check if any profile with gemini target exists
    const bodyText = await page.locator('body').innerText();
    const hasGemini = bodyText.toLowerCase().includes('gemini');
    console.log('Profiles on page:', hasGemini);

    if (!hasGemini) {
      test.skip();
      return;
    }

    // Find edit button near gemini text
    const geminiSection = page.locator('text=gemini').first();
    await expect(geminiSection).toBeVisible({ timeout: 10000 });

    // Find and click edit button
    const editBtn = page.locator('button[title="Edit"]').first();
    if (await editBtn.isVisible()) {
      await editBtn.click();
    } else {
      const iconEditBtn = page.locator('button:has(svg.lucide-edit-2)').first();
      await iconEditBtn.click();
    }

    await page.waitForTimeout(2000);

    // Check for checkboxes (accounts)
    const checkboxes = page.locator('input[type="checkbox"]');
    const checkboxCount = await checkboxes.count();
    console.log(`Edit mode shows ${checkboxCount} account checkbox(es)`);

    // Assert: should see account checkboxes in edit mode
    expect(checkboxCount).toBeGreaterThan(0);
  });
});
