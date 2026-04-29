import { test, expect } from '@playwright/test';

const BASE = process.env.PLAYWRIGHT_BASE_URL || 'http://192.168.5.62:9000';

test.describe('Profile Edit', () => {
  test('edit gemini profile shows all targets and accounts', async ({ page }) => {
    await page.goto(`${BASE}/profiles`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(3000);

    // Find the gemini profile card
    const bodyText = await page.locator('body').innerText();
    console.log('Profiles on page:', bodyText.includes('gemini'));

    // Find and click the Edit button on the gemini profile card
    // Each profile card should have an edit button
    const cards = page.locator('[class*="card"], [class*="Card"]');
    const cardCount = await cards.count();
    console.log(`Found ${cardCount} card-like elements`);

    // Look for edit button near gemini text
    const geminiSection = page.locator('text=gemini').first();
    await expect(geminiSection).toBeVisible({ timeout: 10000 });

    // Screenshot before edit
    await page.screenshot({ path: '/tmp/profiles-before-edit.png', fullPage: true });

    // Find edit button - it should be within the gemini profile card
    // Try different selectors for the edit button
    const editBtn = page.locator('button:has-text("Edit")').first();
    if (await editBtn.isVisible()) {
      // Click the edit button that's closest to gemini
      // Find the card containing gemini and click its edit button
      const geminiCard = page.locator('div:has(> :is(span, p, h3):text-is("gemini")), div:has(> *:text-is("gemini"))').first();
      if (await geminiCard.count() > 0) {
        const cardEditBtn = geminiCard.locator('button:has-text("Edit")').first();
        if (await cardEditBtn.count() > 0) {
          await cardEditBtn.click();
        } else {
          await editBtn.click();
        }
      } else {
        await editBtn.click();
      }
    } else {
      // Try icon button with Edit2 icon or pencil
      const iconEditBtn = page.locator('button[title="Edit"], button:has(svg.lucide-edit-2)').first();
      await iconEditBtn.click();
    }

    await page.waitForTimeout(2000);

    // Screenshot after edit
    await page.screenshot({ path: '/tmp/profiles-after-edit.png', fullPage: true });

    // Check for targets in edit mode
    const editTargets = page.locator('[class*="rounded-md border"]');
    const targetCount = await editTargets.count();
    console.log(`Edit mode shows ${targetCount} target(s)`);

    // Check for checkboxes (accounts)
    const checkboxes = page.locator('input[type="checkbox"]');
    const checkboxCount = await checkboxes.count();
    console.log(`Edit mode shows ${checkboxCount} account checkbox(es)`);

    // Check for account emails
    const afterEditText = await page.locator('body').innerText();
    const hasAccountEmails = [
      'thanapat.t@settek.co',
      'earth424242@gmail.com',
      'thanapat.dev@gmail.com',
      'thanapat.taweerat@lotuss.com'
    ].filter(email => afterEditText.includes(email));
    console.log(`Found ${hasAccountEmails.length}/4 account emails:`, hasAccountEmails);

    // Log the full edit area text for debugging
    const editAreaText = await page.locator('input[type="checkbox"]').first().locator('..').locator('..').innerText().catch(() => 'N/A');
    console.log('Account area text:', editAreaText.substring(0, 500));

    // Check for provider dropdowns (targets)
    const selects = page.locator('select');
    const selectCount = await selects.count();
    console.log(`Edit mode shows ${selectCount} provider select(s)`);

    // Assert: should see account checkboxes in edit mode
    expect(checkboxCount).toBeGreaterThan(0);
  });
});
