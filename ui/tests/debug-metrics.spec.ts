import { test, expect } from '@playwright/test';

test.describe('Debug Metrics Page', () => {
  test('renders page with all sections', async ({ page }) => {
    await page.goto('/debug');
    await expect(page.getByRole('heading', { name: 'Debug Metrics' })).toBeVisible();
    await expect(page.getByText('Mock Data Controls')).toBeVisible();
    await expect(page.getByText('Waste Findings')).toBeVisible();
    await expect(page.getByText('Live Prometheus Metrics')).toBeVisible();
  });

  test('has mock control buttons', async ({ page }) => {
    await page.goto('/debug');
    await expect(page.getByRole('button', { name: 'Seed Once' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Start Loop' })).toBeVisible();
    await expect(page.getByText('Loop Idle')).toBeVisible();
  });

test('has category selector buttons', async ({ page }) => {
  await page.goto('/debug');
  for (const cat of ['All', 'Optimizer', 'Waste', 'Budget']) {
    await expect(page.getByRole('button', { name: cat })).toBeVisible();
  }
});

test('category selection changes seed request', async ({ page }) => {
  await page.goto('/debug');
  await page.getByRole('button', { name: 'Waste' }).click();

  const seedReq = page.waitForRequest(req =>
    req.url().includes('/v1/mock/seed?category=waste') && req.method() === 'POST',
  );
  await page.getByRole('button', { name: 'Seed Once' }).click();
  await seedReq;
});

  test('has waste scan button', async ({ page }) => {
    await page.goto('/debug');
    await expect(page.getByRole('button', { name: 'Scan' })).toBeVisible();
  });

  test('has metrics load button and filter', async ({ page }) => {
    await page.goto('/debug');
    await expect(page.getByRole('button', { name: 'Load' })).toBeVisible();
    await expect(page.getByPlaceholder('Filter metric name...')).toBeVisible();
  });
});

test.describe('Debug Metrics Interactions', () => {
  test('seed mock data button fires request', async ({ page }) => {
    const seedReq = page.waitForRequest(req =>
      req.url().includes('/v1/mock/seed') && req.method() === 'POST',
    );
    await page.goto('/debug');
    await page.getByRole('button', { name: 'Seed Once' }).click();
    await seedReq;
  });

  test('start/stop loop toggles state', async ({ page }) => {
    // Mock the API responses
    await page.route('**/v1/mock/status', route =>
      route.fulfill({ json: { running: false } }),
    );
    await page.route('**/v1/mock/loop/start', route =>
      route.fulfill({ json: { status: 'started' } }),
    );
    await page.route('**/v1/mock/loop/stop', route =>
      route.fulfill({ json: { status: 'stopped' } }),
    );

    await page.goto('/debug');
    await expect(page.getByText('Loop Idle')).toBeVisible();

    // Start loop
    await page.getByRole('button', { name: 'Start Loop' }).click();
    await expect(page.getByText('Loop Running')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Stop Loop' })).toBeVisible();

    // Stop loop
    await page.getByRole('button', { name: 'Stop Loop' }).click();
    await expect(page.getByText('Loop Idle')).toBeVisible();
  });

  test('metrics filter narrows results', async ({ page }) => {
    await page.route('**/api/metrics', route =>
      route.fulfill({
        contentType: 'text/plain',
        body: [
          '# HELP api_gateway_test test',
          'api_gateway_test_metric{model="glm-5"} 42',
          'api_gateway_other_metric{model="glm-4"} 10',
        ].join('\n'),
      }),
    );

    await page.goto('/debug');
    await page.getByRole('button', { name: 'Load' }).click();
    await expect(page.getByText('Showing 2 metrics')).toBeVisible();

    // Filter to only "test" metrics
    await page.getByPlaceholder('Filter metric name...').fill('test');
    await expect(page.getByText('Showing 1 metric')).toBeVisible();
    await expect(page.getByText('api_gateway_test_metric')).toBeVisible();
    await expect(page.getByText('api_gateway_other_metric')).not.toBeVisible();
  });

  test('waste findings display with severity colors', async ({ page }) => {
    await page.route('**/v1/waste/findings', route =>
      route.fulfill({
        json: [
          {
            detector: 'retry_churn',
            severity: 'high',
            message: 'Retry churn detected',
            tokens_wasted: 50000,
            suggestion: 'Fix retry logic',
          },
          {
            detector: 'oversized_context',
            severity: 'medium',
            message: 'Oversized context detected',
            tokens_wasted: 100000,
            suggestion: 'Enable truncation',
          },
        ],
      }),
    );

    await page.goto('/debug');
    await page.getByRole('button', { name: 'Scan' }).click();
    await expect(page.getByText('retry_churn')).toBeVisible();
    await expect(page.getByText('oversized_context')).toBeVisible();
    await expect(page.getByText('50,000 tokens wasted')).toBeVisible();
    await expect(page.getByText('100,000 tokens wasted')).toBeVisible();
  });
});

test.describe('Debug Navigation', () => {
  test('sidebar has Debug link', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Debug' })).toBeVisible();
  });

  test('navigates to debug page from sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Debug' }).click();
    await expect(page.getByRole('heading', { name: 'Debug Metrics' })).toBeVisible();
  });
});
