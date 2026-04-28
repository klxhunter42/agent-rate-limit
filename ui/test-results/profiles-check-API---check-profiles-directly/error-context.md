# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: profiles-check.spec.ts >> API - check profiles directly
- Location: tests/profiles-check.spec.ts:23:1

# Error details

```
TypeError: JSON.stringify is not a function
```

# Test source

```ts
  1  | import { test, expect } from '@playwright/test';
  2  | 
  3  | test('profiles page - wait for load and screenshot', async ({ page }) => {
  4  |   await page.goto('/profiles', { waitUntil: 'networkidle' });
  5  | 
  6  |   // Wait for loading to finish - either profiles appear or "no profiles" text
  7  |   await page.waitForTimeout(5000);
  8  | 
  9  |   const bodyText = await page.locator('body').innerText();
  10 |   console.log('Page text (first 1000 chars):', bodyText.substring(0, 1000));
  11 | 
  12 |   // Check for [[PERSON_18]]
  13 |   const hasPerson = bodyText.includes('[[PERSON_18]]');
  14 |   console.log('Has [[PERSON_18]]:', hasPerson);
  15 | 
  16 |   // Check for any profile names
  17 |   const hasThProfile = bodyText.includes('th15011880');
  18 |   console.log('Has th15011880 profile:', hasThProfile);
  19 | 
  20 |   await page.screenshot({ path: '/tmp/profiles-page2.png', fullPage: true });
  21 | });
  22 | 
  23 | test('API - check profiles directly', async ({ request }) => {
  24 |   const resp = await request.get('http://10.11.11.89:9000/v1/profiles');
  25 |   const data = await resp.json();
  26 |   console.log('Profiles API response:', JSON(data));
  27 | });
  28 | 
  29 | test('API - try delete via POST', async ({ request }) => {
  30 |   const resp = await request.post('http://10.11.11.89:9000/v1/profiles/delete', {
  31 |     data: { name: '[[PERSON_18]]' },
  32 |   });
  33 |   console.log('Delete response status:', resp.status());
  34 |   const text = await resp.text();
  35 |   console.log('Delete response body:', text);
  36 | });
  37 | 
  38 | function JSON(data: any): string {
> 39 |   return JSON.stringify(data, null, 2);
     |               ^ TypeError: JSON.stringify is not a function
  40 | }
  41 | 
```