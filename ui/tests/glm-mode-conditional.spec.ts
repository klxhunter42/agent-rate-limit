import { test, expect } from '@playwright/test';

test.describe('GLM_MODE conditional UI', () => {
 const GLM_ONLY_NAV = ['Model Limits', 'Key Pool', 'Controls'];

 async function detectGlmMode(page) {
 // Wait for dashboard data to load, then check if "Global Capacity" card exists
 await page.waitForTimeout(2000);
 const hasGlobalCapacity = await page.getByText('Global Capacity').first().isVisible().catch(() => false);
 return hasGlobalCapacity;
 }

 test('nav items match glmMode state', async ({ page }) => {
 await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible({ timeout: 10000 });

 const glmMode = await detectGlmMode(page);
 const sidebar = page.locator('[data-slot="sidebar-content"]');

 // Always-visible nav items
 const alwaysVisible = ['Overview', 'Health', 'Analytics', 'Providers', 'Profiles', 'Privacy', 'Models', 'Logs', 'Settings'];
 for (const label of alwaysVisible) {
 await expect(sidebar.getByRole('link', { name: label })).toBeVisible({ timeout: 10000 });
 }

 for (const label of GLM_ONLY_NAV) {
 if (glmMode) {
 await expect(sidebar.getByRole('link', { name: label })).toBeVisible();
 } else {
 await expect(sidebar.getByRole('link', { name: label })).not.toBeVisible();
 }
 }
 });

 test('overview sections match glmMode state', async ({ page }) => {
 await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible({ timeout: 10000 });
 const glmMode = await detectGlmMode(page);

 // Stat cards: only visible in GLM mode
 if (glmMode) {
 await expect(page.getByText('Status').first()).toBeVisible();
 await expect(page.getByText('Queue Depth').first()).toBeVisible();
 await expect(page.getByText('Total Requests').first()).toBeVisible();
 await expect(page.getByText('Concurrency').first()).toBeVisible();
 } else {
 // Stat cards grid (md:grid-cols-4) should not exist when GLM mode is off
 await expect(page.locator('.md\\:grid-cols-4')).not.toBeVisible();
 }

 if (glmMode) {
 await expect(page.getByText('Global Capacity').first()).toBeVisible({ timeout: 10000 });
 await expect(page.getByText('Model Utilization').first()).toBeVisible({ timeout: 10000 });
 } else {
 await expect(page.getByText('Global Capacity')).not.toBeVisible();
 await expect(page.getByText('Model Utilization')).not.toBeVisible();
 }

 // LiveAuthMonitor always visible
 await expect(page.getByText('Auth Monitor').first()).toBeVisible({ timeout: 10000 });
 });
});
