# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: ui/tests/dashboard.spec.ts >> Dashboard Pages >> providers page renders with all providers including OpenRouter
- Location: ui/tests/dashboard.spec.ts:113:3

# Error details

```
Error: page.goto: Protocol error (Page.navigate): Cannot navigate to invalid URL
Call log:
  - navigating to "/providers", waiting until "load"

```

# Test source

```ts
  14  |     await expect(sidebar.getByRole('link', { name: 'Providers' })).toBeVisible();
  15  |     await expect(sidebar.getByRole('link', { name: 'Models' })).toBeVisible();
  16  |     await expect(sidebar.getByRole('link', { name: 'Logs' })).toBeVisible();
  17  |     await expect(sidebar.getByRole('link', { name: 'Profiles' })).toBeVisible();
  18  |     await expect(sidebar.getByRole('link', { name: 'Quota' })).toBeVisible();
  19  |     await expect(sidebar.getByRole('link', { name: 'Settings' })).toBeVisible();
  20  |   });
  21  | });
  22  | 
  23  | test.describe('Dashboard Pages', () => {
  24  |   test('overview page renders', async ({ page }) => {
  25  |     await page.goto('/');
  26  |     await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  27  |     await expect(page.getByText('Status').first()).toBeVisible();
  28  |     await expect(page.getByText('Queue Depth').first()).toBeVisible();
  29  |     await expect(page.getByText('Total Requests').first()).toBeVisible();
  30  |     await expect(page.getByText('Concurrency').first()).toBeVisible();
  31  |   });
  32  | 
  33  |   test('overview page has live auth monitor section', async ({ page }) => {
  34  |     await page.goto('/');
  35  |     await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible({ timeout: 10000 });
  36  |     await expect(page.getByText('Auth Monitor').first()).toBeVisible({ timeout: 10000 });
  37  |   });
  38  | 
  39  |   test('model limits page renders table', async ({ page }) => {
  40  |     await page.goto('/model-limits');
  41  |     await expect(page.getByRole('heading', { name: 'Model Limits' })).toBeVisible();
  42  |     await expect(page.getByRole('columnheader', { name: 'Model' })).toBeVisible();
  43  |     await expect(page.getByRole('columnheader', { name: 'In-Flight' })).toBeVisible();
  44  |     await expect(page.getByRole('columnheader', { name: 'Limit' })).toBeVisible();
  45  |   });
  46  | 
  47  |   test('key pool page renders', async ({ page }) => {
  48  |     await page.goto('/key-pool');
  49  |     await expect(page.getByRole('heading', { name: 'Key Pool' })).toBeVisible();
  50  |   });
  51  | 
  52  |   test('metrics page renders', async ({ page }) => {
  53  |     await page.goto('/');
  54  |     await page.getByRole('link', { name: 'Metrics' }).click();
  55  |     await expect(page.getByRole('heading', { name: 'Metrics' })).toBeVisible({ timeout: 10000 });
  56  |   });
  57  | 
  58  |   test('controls page renders override form and routing strategy', async ({ page }) => {
  59  |     await page.goto('/controls');
  60  |     await expect(page.getByRole('heading', { name: 'Controls' })).toBeVisible();
  61  |     await expect(page.getByText('Manual Override')).toBeVisible();
  62  |     await expect(page.getByText('Routing Strategy').first()).toBeVisible({ timeout: 10000 });
  63  |   });
  64  | 
  65  |   test('controls page routing strategy toggle buttons', async ({ page }) => {
  66  |     await page.goto('/controls');
  67  |     await expect(page.getByRole('heading', { name: 'Controls' })).toBeVisible({ timeout: 10000 });
  68  |     await expect(page.getByText('Round Robin').first()).toBeVisible({ timeout: 10000 });
  69  |     await expect(page.getByText('Fill First').first()).toBeVisible({ timeout: 10000 });
  70  |   });
  71  | 
  72  |   test('health page renders gauge and checks', async ({ page }) => {
  73  |     await page.goto('/system-health');
  74  |     await expect(page.getByRole('heading', { name: 'Live Health' })).toBeVisible({ timeout: 10000 });
  75  |     await expect(page.getByText('System Health')).toBeVisible();
  76  |   });
  77  | 
  78  |   test('analytics page renders all sections', async ({ page }) => {
  79  |     await page.goto('/analytics');
  80  |     await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
  81  |     await expect(page.getByText('Total Tokens').first()).toBeVisible();
  82  |     await expect(page.getByText('Total Cost').first()).toBeVisible();
  83  |   });
  84  | 
  85  |   test('analytics page has time range filter', async ({ page }) => {
  86  |     await page.goto('/analytics');
  87  |     await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
  88  |     await expect(page.getByRole('button', { name: '1H' })).toBeVisible();
  89  |     await expect(page.getByRole('button', { name: '6H' })).toBeVisible();
  90  |     await expect(page.getByRole('button', { name: '24H' })).toBeVisible();
  91  |     await expect(page.getByRole('button', { name: '7D' })).toBeVisible();
  92  |     await expect(page.getByRole('button', { name: '30D' })).toBeVisible();
  93  |   });
  94  | 
  95  |   test('analytics page has model cost breakdown table', async ({ page }) => {
  96  |     await page.goto('/analytics');
  97  |     await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
  98  |     await expect(page.getByText('Model Cost Breakdown')).toBeVisible({ timeout: 10000 });
  99  |   });
  100 | 
  101 |   test('analytics page has chart sections', async ({ page }) => {
  102 |     await page.goto('/analytics');
  103 |     await expect(page.getByRole('heading', { name: 'Usage Analytics' })).toBeVisible({ timeout: 10000 });
  104 |     await expect(page.getByText('Model Distribution').first()).toBeVisible({ timeout: 10000 });
  105 |     await expect(page.getByText('Token Breakdown').first()).toBeVisible({ timeout: 10000 });
  106 |   });
  107 | 
  108 |   test('privacy page renders', async ({ page }) => {
  109 |     await page.goto('/privacy');
  110 |     await expect(page.locator('h1, .text-red-500').first()).toBeVisible({ timeout: 10000 });
  111 |   });
  112 | 
  113 |   test('providers page renders with all providers including OpenRouter', async ({ page }) => {
> 114 |     await page.goto('/providers');
      |                ^ Error: page.goto: Protocol error (Page.navigate): Cannot navigate to invalid URL
  115 |     await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible({ timeout: 10000 });
  116 |     await expect(page.getByText('Z.AI')).toBeVisible();
  117 |     await expect(page.getByText('Anthropic')).toBeVisible();
  118 |     await expect(page.getByText('Gemini', { exact: true })).toBeVisible();
  119 |     await expect(page.getByText('Gemini (OAuth)')).toBeVisible();
  120 |     await expect(page.getByText('OpenAI')).toBeVisible();
  121 |     await expect(page.getByText('GitHub Copilot')).toBeVisible();
  122 |     await expect(page.getByText('OpenRouter')).toBeVisible();
  123 |   });
  124 | 
  125 |   test('models page renders with search', async ({ page }) => {
  126 |     await page.goto('/models');
  127 |     await expect(page.getByRole('heading', { name: 'Model Catalog' })).toBeVisible({ timeout: 10000 });
  128 |   });
  129 | 
  130 |   test('logs page renders error log viewer', async ({ page }) => {
  131 |     await page.goto('/logs');
  132 |     await expect(page.getByRole('heading', { name: 'Error Logs' })).toBeVisible({ timeout: 10000 });
  133 |   });
  134 | 
  135 |   test('settings page renders', async ({ page }) => {
  136 |     await page.goto('/settings');
  137 |     await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10000 });
  138 |     await expect(page.getByText('General')).toBeVisible();
  139 |     await expect(page.getByText('Notifications')).toBeVisible();
  140 |     await expect(page.getByText('Language').first()).toBeVisible();
  141 |   });
  142 | });
  143 | 
  144 | test.describe('Theme Toggle', () => {
  145 |   test('dark mode toggle works', async ({ page }) => {
  146 |     await page.goto('/');
  147 | 
  148 |     const html = page.locator('html');
  149 |     const isDark = await html.evaluate(() => document.documentElement.classList.contains('dark'));
  150 | 
  151 |     const themeBtn = page.locator('[data-slot="sidebar-footer"] button').first();
  152 |     await themeBtn.click();
  153 | 
  154 |     const isDarkAfter = await html.evaluate(() => document.documentElement.classList.contains('dark'));
  155 |     expect(isDarkAfter).toBe(!isDark);
  156 |   });
  157 | });
  158 | 
  159 | test.describe('SPA Routing', () => {
  160 |   test('unknown route redirects to overview', async ({ page }) => {
  161 |     await page.goto('/nonexistent');
  162 |     await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  163 |   });
  164 | 
  165 |   test('direct navigation to model-limits works', async ({ page }) => {
  166 |     await page.goto('/model-limits');
  167 |     await expect(page.getByRole('heading', { name: 'Model Limits' })).toBeVisible();
  168 |   });
  169 | 
  170 |   test('direct navigation to models page works', async ({ page }) => {
  171 |     await page.goto('/models');
  172 |     await expect(page.getByRole('heading', { name: 'Model Catalog' })).toBeVisible({ timeout: 10000 });
  173 |   });
  174 | 
  175 |   test('direct navigation to logs page works', async ({ page }) => {
  176 |     await page.goto('/logs');
  177 |     await expect(page.getByRole('heading', { name: 'Error Logs' })).toBeVisible({ timeout: 10000 });
  178 |   });
  179 | });
  180 | 
  181 | test.describe('New Pages', () => {
  182 |   test('profiles page renders', async ({ page }) => {
  183 |     await page.goto('/profiles');
  184 |     await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible({ timeout: 10000 });
  185 |   });
  186 | 
  187 |   test('quota page renders', async ({ page }) => {
  188 |     await page.goto('/quota');
  189 |     await expect(page.getByRole('heading', { name: 'Quota' })).toBeVisible({ timeout: 10000 });
  190 |   });
  191 | 
  192 |   test('settings page has server config section', async ({ page }) => {
  193 |     await page.goto('/settings');
  194 |     await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10000 });
  195 |     await expect(page.getByText('Server Config', { exact: true })).toBeVisible({ timeout: 10000 });
  196 |   });
  197 | });
  198 | 
```