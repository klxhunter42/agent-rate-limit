import { test, expect } from '@playwright/test';

test.describe('Lotus LLM Provider', () => {
  test('lotus card is visible on providers page', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Lotus LLM')).toBeVisible();
  });

  test('lotus card shows API Key badge', async ({ page }) => {
    await page.goto('/providers');
    const lotusCard = page.locator('div.grid').getByText('Lotus LLM').locator('..');
    await expect(lotusCard.getByText('API Key')).toBeVisible();
  });

  test('lotus has Connect button', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByText('Lotus LLM')).toBeVisible({ timeout: 10000 });
    const card = page.locator('div.grid > div').filter({ hasText: 'Lotus LLM' });
    await expect(card.getByRole('button').first()).toBeVisible();
  });
});
