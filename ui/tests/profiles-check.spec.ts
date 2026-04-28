import { test, expect } from '@playwright/test';

test('profiles page - wait for load and screenshot', async ({ page }) => {
  await page.goto('/profiles', { waitUntil: 'networkidle' });

  // Wait for loading to finish - either profiles appear or "no profiles" text
  await page.waitForTimeout(5000);

  const bodyText = await page.locator('body').innerText();
  console.log('Page text (first 1000 chars):', bodyText.substring(0, 1000));

  // Check for [[PERSON_18]]
  const hasPerson = bodyText.includes('[[PERSON_18]]');
  console.log('Has [[PERSON_18]]:', hasPerson);

  // Check for any profile names
  const hasThProfile = bodyText.includes('th15011880');
  console.log('Has th15011880 profile:', hasThProfile);

  await page.screenshot({ path: '/tmp/profiles-page2.png', fullPage: true });
});

test('API - check profiles directly', async ({ request }) => {
  const resp = await request.get('http://10.11.11.89:9000/v1/profiles');
  const data = await resp.json();
  console.log('Profiles API response:', JSON(data));
});

test('API - try delete via POST', async ({ request }) => {
  const resp = await request.post('http://10.11.11.89:9000/v1/profiles/delete', {
    data: { name: '[[PERSON_18]]' },
  });
  console.log('Delete response status:', resp.status());
  const text = await resp.text();
  console.log('Delete response body:', text);
});

function JSON(data: any): string {
  return JSON.stringify(data, null, 2);
}
