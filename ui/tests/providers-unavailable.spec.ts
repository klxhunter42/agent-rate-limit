import { test, expect } from '@playwright/test';

const DASHBOARD_PASSWORD = process.env.DASHBOARD_PASSWORD || 'klxhunter';

test.beforeEach(async ({ page }) => {
  const res = await page.request.post('/v1/auth/login', {
    data: { password: DASHBOARD_PASSWORD },
  });
  if (!res.ok()) throw new Error(`Login failed: ${res.status()}`);
});

const UNAVAILABLE_PROVIDERS = [
 'Anthropic',
 'OpenAI',
 'Gemini',  // exact match to avoid hitting Gemini (OAuth)
 'OpenRouter',
 'GitHub Copilot',
 'DeepSeek',
 'HuggingFace',
 'Ollama',
 'AGY',
 'Cursor',
 'CodeBuddy',
 'Kilo',
];

const AVAILABLE_PROVIDERS = [
 'Claude (OAuth)',
 'Gemini (OAuth)',
 'Kimi',
];

test.describe('Provider Unavailable Status', () => {
 test.beforeEach(async ({ page }) => {
 await page.goto('/providers');
 await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible({ timeout: 10000 });
 });

 test('unavailable providers show Unavailable badge', async ({ page }) => {
 for (const name of UNAVAILABLE_PROVIDERS) {
 const cardText = name === 'Gemini' ? page.getByText('Gemini', { exact: true }).first() : page.getByText(name).first();
 await expect(cardText).toBeVisible();
 const card = cardText.locator('xpath=ancestor::*[contains(@class,"border-transparent")]');
 await expect(card.getByText('Unavailable')).toBeVisible();
 }
 });

 test('available providers do not show Unavailable badge', async ({ page }) => {
 for (const name of AVAILABLE_PROVIDERS) {
 const cardText = page.getByText(name).first();
 await expect(cardText).toBeVisible();
 // Scope to the card container (border-transparent)
 const card = cardText.locator('xpath=ancestor::*[contains(@class,"border-transparent")]');
 await expect(card.locator('text=Unavailable')).toHaveCount(0);
 }
 });

 test('unavailable providers have no Connect/Add button', async ({ page }) => {
 for (const name of UNAVAILABLE_PROVIDERS) {
 const cardText = name === 'Gemini' ? page.getByText('Gemini', { exact: true }).first() : page.getByText(name).first();
 await expect(cardText).toBeVisible();
 const card = cardText.locator('xpath=ancestor::*[contains(@class,"border-transparent")]');
 const btn = card.getByRole('button', { name: /Connect|Add/ });
 await expect(btn).toHaveCount(0);
 }
 });

 test('available providers have Connect or Add button', async ({ page }) => {
 for (const name of AVAILABLE_PROVIDERS) {
 const cardText = page.getByText(name).first();
 await expect(cardText).toBeVisible();
 const header = cardText.locator('xpath=ancestor::div[contains(@class,"flex items-center gap-3")]');
 const btn = header.getByRole('button', { name: /Connect|Add/ });
 await expect(btn).toBeVisible();
 }
 });

 test('all providers render on page', async ({ page }) => {
 const allProviders = [...UNAVAILABLE_PROVIDERS, ...AVAILABLE_PROVIDERS];
 for (const name of allProviders) {
 await expect(page.getByText(name).first()).toBeVisible();
 }
 });
});
