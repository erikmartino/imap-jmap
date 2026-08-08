import { defineConfig } from '@playwright/test';

const baseURL = process.env.BULWARK_BASE_URL ?? 'http://localhost:3000';
const jmapURL = process.env.JMAP_SERVER_URL ?? 'https://localhost:8443';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? [['line'], ['html', { open: 'never' }]] : 'list',
  timeout: 60_000,
  expect: { timeout: 15_000 },
  globalSetup: './global-setup.ts',
  // Keep every test's output artifacts — including traces for succeeded tests —
  // rather than pruning passing runs (default is 'always'; pinned so it cannot
  // regress to 'failures-only' in CI).
  preserveOutput: 'always',
  use: {
    baseURL,
    headless: true,
    ignoreHTTPSErrors: true,
    // 'on' records and retains a trace for EVERY test, pass or fail (open with
    // `pnpm exec playwright show-trace test-results/<test>/trace.zip`).
    trace: 'on',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    navigationTimeout: 30_000,
    launchOptions: {
      args: ['--no-sandbox'],
    },
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  metadata: { baseURL, jmapURL },
});
