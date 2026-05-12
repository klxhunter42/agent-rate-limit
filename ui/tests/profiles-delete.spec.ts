import { test, expect } from '@playwright/test';

test.describe('Delete Profile', () => {
 test('delete profile via API', async ({ request }) => {
 const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:9000';
 const name = 'test-delete-' + Date.now();

 // Create profile
 const createResp = await request.post(`${baseURL}/v1/profiles`, {
 data: { name, target: 'claude-oauth' },
 });
 expect(createResp.status()).toBe(201);

 // Delete profile
 const deleteResp = await request.post(`${baseURL}/v1/profiles/delete`, {
 data: { name },
 });
 expect([200, 204]).toContain(deleteResp.status());

 // Verify removed
 const listResp = await request.get(`${baseURL}/v1/profiles`);
 expect(listResp.ok()).toBeTruthy();
 const data = await listResp.json();
 const names = (data.profiles as any[]).map((p: any) => p.name);
 expect(names).not.toContain(name);
 });
});
