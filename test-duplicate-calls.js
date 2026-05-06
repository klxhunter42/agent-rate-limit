const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();

  const BASE = 'http://localhost:9000';

  // Track all requests
  const apiCalls = {};
  page.on('request', req => {
    const url = new URL(req.url());
    const path = url.pathname;
    if (path.startsWith('/api/') || path.startsWith('/v1/') || path === '/health') {
      if (!apiCalls[path]) apiCalls[path] = 0;
      apiCalls[path]++;
    }
  });

  console.log('1. Loading dashboard...');
  await page.goto(`${BASE}/dashboard/`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(2000);

  console.log('\n2. API calls during dashboard load:');
  const sorted = Object.entries(apiCalls).sort((a, b) => a[0].localeCompare(b[0]));
  for (const [path, count] of sorted) {
    const status = count > 1 ? `DUPLICATE (${count}x)` : 'OK';
    console.log(`  ${path}: ${count}x  [${status}]`);
  }

  console.log('\n3. Clicking Overview tab...');
  apiCalls['/v1/limiter-status'] = 0;
  apiCalls['/health'] = 0;
  apiCalls['/api/metrics'] = 0;
  await page.getByText('Overview').click();
  await page.waitForTimeout(3000);

  console.log('\n4. API calls after Overview click:');
  for (const path of ['/v1/limiter-status', '/health', '/api/metrics']) {
    const count = apiCalls[path] || 0;
    const status = count > 1 ? `DUPLICATE (${count}x)` : 'OK';
    console.log(`  ${path}: ${count}x  [${status}]`);
  }

  // Summary
  const allCalls = Object.values(apiCalls);
  const duplicates = Object.entries(apiCalls).filter(([, c]) => c > 1);
  console.log(`\n5. Summary:`);
  console.log(`  Total unique endpoints: ${Object.keys(apiCalls).length}`);
  console.log(`  Duplicates found: ${duplicates.length > 0 ? 'YES' : 'NO'}`);
  if (duplicates.length > 0) {
    for (const [path, count] of duplicates) {
      console.log(`  - ${path}: ${count}x`);
    }
  }

  await browser.close();
  process.exit(duplicates.length > 0 ? 1 : 0);
})();
