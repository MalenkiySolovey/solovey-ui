import { defineConfig, devices } from '@playwright/test'

const manageTestServer = process.env.SUI_E2E_SKIP_WEB_SERVER !== '1'

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: manageTestServer ? './tests/e2e/global-setup.ts' : undefined,
  globalTeardown: manageTestServer ? './tests/e2e/global-teardown.ts' : undefined,
  timeout: 45_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  workers: 1,
  outputDir: '../tests/baseline/phase6/playwright/test-results',
  reporter: [
    ['list'],
    ['junit', { outputFile: '../tests/baseline/phase6/playwright.junit.xml' }],
    ['html', { outputFolder: '../tests/baseline/phase6/playwright/html', open: 'never' }],
  ],
  use: {
    baseURL: process.env.SUI_E2E_BASE_URL ?? 'http://127.0.0.1:3000/app/',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
