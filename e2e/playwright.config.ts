import { defineConfig } from '@playwright/test';

const baseURL = process.env.BULWARK_BASE_URL ?? 'http://localhost:3000';
const jmapURL = process.env.JMAP_SERVER_URL ?? 'http://localhost:8080';

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
  use: {
    baseURL,
    headless: true,
    ignoreHTTPSErrors: true,
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
