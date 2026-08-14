import { expect, type Page } from '@playwright/test'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'

export type E2EServerState = {
  baseURL: string
  backendURL: string
  username: string
  password: string
  dbDir: string
}

export const repoRoot = path.resolve(process.cwd(), '..')
export const e2eResultDir = path.join(repoRoot, 'tests', 'baseline', 'e2e')
export const serverStatePath = path.join(e2eResultDir, 'e2e-server', 'state.json')
const fallbackE2EWebPath = normalizeWebPath(process.env.SUI_E2E_WEB_PATH ?? '/e2e-panel/')

export const readServerState = (): E2EServerState => {
  if (fs.existsSync(serverStatePath)) {
    const state = JSON.parse(fs.readFileSync(serverStatePath, 'utf8')) as E2EServerState
    testPasswordAvailable(state)
    return state
  }

  const state = {
    baseURL: process.env.SUI_E2E_BASE_URL ?? `http://127.0.0.1:3000${fallbackE2EWebPath}`,
    backendURL: process.env.SUI_E2E_BACKEND_URL ?? `http://127.0.0.1:2095${fallbackE2EWebPath}`,
    username: process.env.SUI_E2E_USERNAME ?? 'admin',
    password: process.env.SUI_E2E_PASSWORD ?? '',
    dbDir: process.env.SUI_E2E_DB_DIR ?? path.join(e2eResultDir, 'e2e-db'),
  }
  testPasswordAvailable(state)
  return state
}

function normalizeWebPath(value: string): string {
  const trimmed = value.trim()
  if (!trimmed || trimmed === '/') return '/e2e-panel/'
  return `${trimmed.startsWith('/') ? '' : '/'}${trimmed}${trimmed.endsWith('/') ? '' : '/'}`
}

export const setEnglishLocale = async (page: Page) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('locale', 'en')
  })
}

export const login = async (page: Page) => {
  const state = readServerState()
  testPasswordAvailable(state)
  await setEnglishLocale(page)
  await page.goto('login')
  const inputs = page.locator('input')
  await inputs.nth(0).fill(state.username)
  await inputs.nth(1).fill(state.password)
  const loginResponsePromise = page.waitForResponse(response => (
    response.request().method() === 'POST' && response.url() === new URL('api/login', state.baseURL).toString()
  ))
  await page.locator('button[type="submit"]').click()
  const loginResponse = await loginResponsePromise
  const loginBody = await loginResponse.json().catch(() => ({ success: false, msg: 'invalid login response' }))
  expect(loginResponse.ok(), loginBody.msg).toBeTruthy()
  expect(loginBody.success, loginBody.msg).toBe(true)
  expect(loginBody.obj?.state).toBe('authenticated')
  await expect(page).toHaveURL(state.baseURL)

  const settingsResponse = await page.request.get('api/settings')
  const settingsBody = await settingsResponse.json().catch(() => ({ success: false, msg: 'invalid settings response' }))
  expect(settingsResponse.ok(), settingsBody.msg).toBeTruthy()
  expect(settingsBody.success, settingsBody.msg).toBe(true)
}

export const csrfToken = async (page: Page) => {
  const response = await page.request.get('api/csrf')
  expect(response.ok()).toBeTruthy()
  const body = await response.json()
  expect(body.success).toBeTruthy()
  expect(typeof body.obj?.token).toBe('string')
  return body.obj.token as string
}

export const mutationHeaders = (token?: string) => ({
  Origin: new URL(readServerState().baseURL).origin,
  'X-Requested-With': 'XMLHttpRequest',
  ...(token ? { 'X-CSRF-Token': token } : {}),
})

export const stepUpMutationHeaders = async (page: Page, operationKind: string, target: string) => {
  const csrf = await csrfToken(page)
  const response = await page.request.post('api/v1/security/step-up', {
    headers: mutationHeaders(csrf),
    data: {
      method: 'password',
      credential: readServerState().password,
      operationKind,
      targetDigest: createHash('sha256').update(target).digest('hex'),
    },
  })
  const body = await response.json().catch(() => ({ success: false, msg: response.statusText() }))
  expect(response.ok(), body.msg).toBeTruthy()
  expect(body.success, body.msg).toBe(true)
  expect(typeof body.obj?.token).toBe('string')

  return {
    ...mutationHeaders(await csrfToken(page)),
    'X-Step-Up-Token': body.obj.token as string,
  }
}

export const enableComponent = async (page: Page, id: string) => {
  const token = await csrfToken(page)
  const response = await page.request.post(`api/update/components/${id}/enable`, {
    headers: mutationHeaders(token),
  })
  const body = await response.json().catch(() => ({ success: false, msg: response.statusText() }))
  expect(response.ok(), body.msg).toBeTruthy()
  expect(body.success, body.msg).toBe(true)
  await page.goto('')
  await page.reload()
}

export const writeJSONArtifact = (relativePath: string, value: unknown) => {
  const target = path.join(e2eResultDir, relativePath)
  fs.mkdirSync(path.dirname(target), { recursive: true })
  fs.writeFileSync(target, JSON.stringify(value, null, 2))
}

export const hasImportFixtures = () => (
  fs.existsSync(path.join(repoRoot, 'test-db', 'x-ui.db')) &&
  fs.existsSync(path.join(repoRoot, 'test-db', 's-ui.db'))
)

const testPasswordAvailable = (state: E2EServerState) => {
  if (!state.password) {
    throw new Error(`E2E password is empty; expected ${serverStatePath} from run-server.js or SUI_E2E_PASSWORD`)
  }
}
