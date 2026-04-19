import { defineConfig } from '@playwright/test';

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173';
const isCI = !!process.env.CI;

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  workers: isCI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL,
    trace: 'on-first-retry',
  },
  ...(isCI
    ? {}
    : {
        webServer: {
          command: 'bun run dev',
          url: 'http://localhost:5173',
          reuseExistingServer: true,
          timeout: 15000,
        },
      }),
});
