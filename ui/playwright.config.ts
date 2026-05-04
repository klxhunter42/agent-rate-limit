import { defineConfig } from '@playwright/test';

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:9000';
const isCI = !!process.env.CI;

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  workers: isCI ? 1 : undefined,
  reporter: 'list',
  projects: [
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'tests',
      testMatch: /.*\.spec\.ts/,
      dependencies: ['setup'],
      use: {
        baseURL,
        trace: 'on-first-retry',
        storageState: '.auth.json',
      },
    },
  ],
});
