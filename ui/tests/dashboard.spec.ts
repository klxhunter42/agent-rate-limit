import { test, expect } from '@playwright/test';

test.describe('Navigation', () => {
  test('sidebar has all nav items', async ({ page }) => {
    await page.goto('/');
    const sidebar = page.locator('[data-slot="sidebar-content"]');
    await expect(sidebar.getByRole('link', { name: 'Overview' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Health' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Model Limits' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Key Pool' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Analytics' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Metrics' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Controls' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Providers' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Models' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Logs' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Profiles' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Quota' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Settings' })).toBeVisible();
  });
});

test.describe('Dashboard Pages', () => {
  test('overview page renders', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
    await expect(page.getByText('Status').first()).toBeVisible();
    await expect(page.getByText('Queue Depth').first()).toBeVisible();
    await expect(page.getByText('Total Requests').first()).toBeVisible();
    await expect(page.getByText('Concurrency').first()).toBeVisible();
  });

  test('overview page has live auth monitor section', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Auth Monitor').first()).toBeVisible({ timeout: 10000 });
  });

  test('model limits page renders table', async ({ page }) => {
    await page.goto('/model-limits');
    await expect(page.getByRole('heading', { name: 'Model Limits' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Model' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'In-Flight' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Limit' })).toBeVisible();
  });

  test('key pool page renders', async ({ page }) => {
    await page.goto('/key-pool');
    await expect(page.getByRole('heading', { name: 'Key Pool' })).toBeVisible();
  });

  test('metrics page renders', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Metrics' }).click();
    await expect(page.getByRole('heading', { name: 'Metrics' })).toBeVisible({ timeout: 10000 });
  });

  test('controls page renders override form and routing strategy', async ({ page }) => {
    await page.goto('/controls');
    await expect(page.getByRole('heading', { name: 'Controls' })).toBeVisible();
    await expect(page.getByText('Manual Override')).toBeVisible();
    await expect(page.getByText('Routing Strategy').first()).toBeVisible({ timeout: 10000 });
  });

  test('controls page routing strategy toggle buttons', async ({ page }) => {
    await page.goto('/controls');
    await expect(page.getByRole('heading', { name: 'Controls' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Round Robin').first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Fill First').first()).toBeVisible({ timeout: 10000 });
  });

  test('health page renders gauge and checks', async ({ page }) => {
    await page.goto('/system-health');
    await expect(page.getByRole('heading', { name: 'Live Health' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('System Health')).toBeVisible();
  });

  test('analytics page renders all sections', async ({ page }) => {
    await page.goto('/analytics');
    await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Total Tokens').first()).toBeVisible();
    await expect(page.getByText('Total Cost').first()).toBeVisible();
  });

  test('analytics page has time range filter', async ({ page }) => {
    await page.goto('/analytics');
    await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: '1H' })).toBeVisible();
    await expect(page.getByRole('button', { name: '6H' })).toBeVisible();
    await expect(page.getByRole('button', { name: '24H' })).toBeVisible();
    await expect(page.getByRole('button', { name: '7D' })).toBeVisible();
    await expect(page.getByRole('button', { name: '30D' })).toBeVisible();
  });

  test('analytics page has model cost breakdown table', async ({ page }) => {
    await page.goto('/analytics');
    await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Model Cost Breakdown')).toBeVisible({ timeout: 10000 });
  });

  test('analytics page has chart sections', async ({ page }) => {
    await page.goto('/analytics');
    await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Model Distribution').first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Token Breakdown').first()).toBeVisible({ timeout: 10000 });
  });

  test('privacy page renders', async ({ page }) => {
    await page.goto('/privacy');
    await expect(page.locator('h1, .text-red-500').first()).toBeVisible({ timeout: 10000 });
  });

  test('providers page renders with all providers including OpenRouter', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Z.AI')).toBeVisible();
    await expect(page.getByText('Anthropic')).toBeVisible();
    await expect(page.getByText('Gemini', { exact: true })).toBeVisible();
    await expect(page.getByText('Gemini (OAuth)')).toBeVisible();
    await expect(page.getByText('OpenAI')).toBeVisible();
    await expect(page.getByText('GitHub Copilot')).toBeVisible();
    await expect(page.getByText('OpenRouter')).toBeVisible();
  });

  test('models page renders with search', async ({ page }) => {
    await page.goto('/models');
    await expect(page.getByRole('heading', { name: 'Model Catalog' })).toBeVisible({ timeout: 10000 });
  });

  test('logs page renders error log viewer', async ({ page }) => {
    await page.goto('/logs');
    await expect(page.getByRole('heading', { name: 'Error Logs' })).toBeVisible({ timeout: 10000 });
  });

  test('settings page renders', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('General')).toBeVisible();
    await expect(page.getByText('Notifications')).toBeVisible();
    await expect(page.getByText('Language').first()).toBeVisible();
  });
});

test.describe('Theme Toggle', () => {
  test('dark mode toggle works', async ({ page }) => {
    await page.goto('/');

    const html = page.locator('html');
    const isDark = await html.evaluate(() => document.documentElement.classList.contains('dark'));

    const themeBtn = page.locator('[data-slot="sidebar-footer"] button').first();
    await themeBtn.click();

    const isDarkAfter = await html.evaluate(() => document.documentElement.classList.contains('dark'));
    expect(isDarkAfter).toBe(!isDark);
  });
});

test.describe('SPA Routing', () => {
  test('unknown route redirects to overview', async ({ page }) => {
    await page.goto('/nonexistent');
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  });

  test('direct navigation to model-limits works', async ({ page }) => {
    await page.goto('/model-limits');
    await expect(page.getByRole('heading', { name: 'Model Limits' })).toBeVisible();
  });

  test('direct navigation to models page works', async ({ page }) => {
    await page.goto('/models');
    await expect(page.getByRole('heading', { name: 'Model Catalog' })).toBeVisible({ timeout: 10000 });
  });

  test('direct navigation to logs page works', async ({ page }) => {
    await page.goto('/logs');
    await expect(page.getByRole('heading', { name: 'Error Logs' })).toBeVisible({ timeout: 10000 });
  });
});

test.describe('New Pages', () => {
  test('profiles page renders', async ({ page }) => {
    await page.goto('/profiles');
    await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible({ timeout: 10000 });
  });

  test('quota page renders', async ({ page }) => {
    await page.goto('/quota');
    await expect(page.getByRole('heading', { name: 'Quota' })).toBeVisible({ timeout: 10000 });
  });

  test('settings page has server config section', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Server Config', { exact: true })).toBeVisible({ timeout: 10000 });
  });
});
