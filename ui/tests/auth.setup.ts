import { test as setup, expect } from '@playwright/test';

const DASHBOARD_PASSWORD = process.env.DASHBOARD_PASSWORD || 'klxhunter';

setup('authenticate', async ({ request }) => {
  const res = await request.post('/v1/auth/login', {
    data: { password: DASHBOARD_PASSWORD },
  });
  expect(res.ok()).toBeTruthy();
  await request.storageState({ path: '.auth.json' });
});
