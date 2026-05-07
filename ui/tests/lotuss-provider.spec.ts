import { test, expect } from '@playwright/test';

test.describe('Lotuss Provider', () => {
  test('lotus card is visible on providers page', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Lotuss')).toBeVisible();
  });

  test('lotus card shows API Key badge', async ({ page }) => {
    await page.goto('/providers');
    const lotusCard = page.locator('div.grid').getByText('Lotuss').locator('..');
    await expect(lotusCard.getByText('API Key')).toBeVisible();
  });

  test('lotus has Connect button', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByText('Lotuss')).toBeVisible({ timeout: 10000 });
    const card = page.locator('div.grid > div').filter({ hasText: 'Lotuss' });
    await expect(card.getByRole('button').first()).toBeVisible();
  });
});
