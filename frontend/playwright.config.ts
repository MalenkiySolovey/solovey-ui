import { defineConfig, devices } from '@playwright/test'

const manageTestServer = process.env.SUI_E2E_SKIP_WEB_SERVER !== '1'
const e2eWebPath = normalizeWebPath(process.env.SUI_E2E_WEB_PATH ?? '/e2e-panel/')

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: manageTestServer ? './tests/e2e/global-setup.ts' : undefined,
  globalTeardown: manageTestServer ? './tests/e2e/global-teardown.ts' : undefined,
  timeout: 45_000,
  expect: {
    timeout: 10_000,
  },
  retries: process.env.CI ? 2 : 0,
  fullyParallel: false,
  workers: 1,
  outputDir: '../tests/baseline/e2e/playwright/test-results',
  reporter: [
    ['list'],
    ['junit', { outputFile: '../tests/baseline/e2e/playwright.junit.xml' }],
    ['html', { outputFolder: '../tests/baseline/e2e/playwright/html', open: 'never' }],
  ],
  use: {
    baseURL: process.env.SUI_E2E_BASE_URL ?? `http://127.0.0.1:3000${e2eWebPath}`,
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

function normalizeWebPath(value: string): string {
  const trimmed = value.trim()
  if (!trimmed || trimmed === '/') return '/e2e-panel/'
  return `${trimmed.startsWith('/') ? '' : '/'}${trimmed}${trimmed.endsWith('/') ? '' : '/'}`
}
