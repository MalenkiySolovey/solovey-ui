const fs = require('node:fs')
const path = require('node:path')
const { randomBytes } = require('node:crypto')
const { spawn, spawnSync } = require('node:child_process')

const repoRoot = path.resolve(__dirname, '..', '..')
const frontendDir = path.join(repoRoot, 'frontend')
const resultDir = path.join(repoRoot, 'tests', 'baseline', 'e2e')
const serverDir = path.join(resultDir, 'e2e-server')
const generatedDir = path.join(serverDir, 'generated')
const dbDir = path.join(resultDir, 'e2e-db')
const appDataDir = path.join(resultDir, 'appdata')
const runtimeDir = path.join(resultDir, '.runtime')
const zigGlobalCacheDir = path.join(resultDir, 'zig-global-cache')
const zigLocalCacheDir = path.join(resultDir, 'zig-local-cache')
const componentsStateDir = path.join(resultDir, 'components')
const statePath = path.join(serverDir, 'state.json')
const stateTempPath = `${statePath}.tmp`
const backendAddressPath = path.join(serverDir, 'backend-address.txt')
const overlayPath = path.join(serverDir, 'go-overlay.json')
const generatedAppPath = path.join(generatedDir, 'components_generated.go')
const generatedCommandPath = path.join(generatedDir, 'optional_commands_generated.go')
const generatedBrokerPath = path.join(generatedDir, 'broker_components_generated.go')
const bundledZig = path.join(repoRoot, '..', '..', '.devtools', 'zig-x86_64-windows-0.16.0', 'zig.exe')
const resolvedCC = process.env.CC || (process.platform === 'win32' && fs.existsSync(bundledZig) ? `${bundledZig} cc` : undefined)
const readyTimeoutMs = Number(process.env.SUI_E2E_READY_TIMEOUT_MS || (process.env.CI ? 900000 : 300000))
const e2eWebPath = normalizeWebPath(process.env.SUI_E2E_WEB_PATH || '/e2e-panel/')

for (const runtimePath of [serverDir, dbDir, appDataDir, runtimeDir, zigGlobalCacheDir, zigLocalCacheDir, componentsStateDir]) {
  fs.rmSync(runtimePath, { recursive: true, force: true })
}
for (const runtimePath of [serverDir, dbDir, appDataDir, runtimeDir, zigGlobalCacheDir, zigLocalCacheDir, componentsStateDir]) {
  fs.mkdirSync(runtimePath, { recursive: true })
}

const logStream = (name) => fs.createWriteStream(path.join(serverDir, `${name}.log`), { flags: 'w' })
const children = []
const childFailures = new WeakMap()

const normalizeSpawnEnv = (env = process.env) => {
  if (process.platform !== 'win32') return env

  const normalized = { ...env }
  const pathKey = Object.keys(normalized).find((key) => key.toLowerCase() === 'path')
  if (!pathKey) return normalized

  const pathValue = normalized[pathKey]
  for (const key of Object.keys(normalized)) {
    if (key.toLowerCase() === 'path') delete normalized[key]
  }
  normalized.Path = pathValue
  return normalized
}

function normalizeWebPath(value) {
  const trimmed = String(value || '').trim()
  if (!trimmed || trimmed === '/') return '/e2e-panel/'
  return `${trimmed.startsWith('/') ? '' : '/'}${trimmed}${trimmed.endsWith('/') ? '' : '/'}`
}

const rememberChildFailure = (child, name, error) => {
  const failure = error instanceof Error ? error : new Error(String(error))
  childFailures.set(child, failure)
  return `[${name}] ${failure.stack || failure.message}\n`
}

const throwIfChildFailed = (watchedChildren) => {
  for (const { name, child } of watchedChildren) {
    const failure = childFailures.get(child)
    if (failure) {
      throw new Error(`${name} failed: ${failure.message}`)
    }
  }
}

let stopped = false

const spawnLogged = (name, command, args, options) => {
  const child = spawn(command, args, {
    ...options,
    env: normalizeSpawnEnv(options?.env),
    detached: process.platform !== 'win32',
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  })
  children.push(child)
  const log = logStream(name)
  child.stdout.on('data', (chunk) => {
    process.stdout.write(chunk)
    log.write(chunk)
  })
  child.stderr.on('data', (chunk) => {
    process.stderr.write(chunk)
    log.write(chunk)
  })
  child.on('error', (error) => {
    const message = rememberChildFailure(child, name, error)
    process.stderr.write(message)
    log.write(message)
  })
  child.on('exit', (code, signal) => {
    log.write(`\n[${name}] exited code=${code} signal=${signal}\n`)
    if (!stopped) {
      const message = rememberChildFailure(child, name, new Error(`exited code=${code} signal=${signal}`))
      process.stderr.write(message)
      log.write(message)
    }
  })
  return child
}

const generateComponentOverlay = () => {
  const generator = path.join(repoRoot, 'scripts', 'generate-component-imports.mjs')
  const result = spawnSync(process.execPath, [
    generator,
    '--profile',
    'full',
    '--out',
    generatedAppPath,
    '--cmd-out',
    generatedCommandPath,
    '--broker-out',
    generatedBrokerPath,
  ], {
    cwd: repoRoot,
    env: normalizeSpawnEnv(process.env),
    stdio: 'inherit',
    windowsHide: true,
  })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`component import generation failed with exit code ${result.status}`)

  const productionAppPath = path.join(repoRoot, 'app', 'components_generated.go')
  fs.writeFileSync(overlayPath, JSON.stringify({
    Replace: {
      [productionAppPath]: generatedAppPath,
    },
  }, null, 2))
}

const waitForFile = async (file, timeoutMs, watchedChildren = []) => {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    throwIfChildFailed(watchedChildren)
    if (fs.existsSync(file)) return fs.readFileSync(file, 'utf8').trim()
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  throw new Error(`Timed out waiting for ${file}`)
}

const waitForURL = async (url, timeoutMs, watchedChildren = []) => {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    throwIfChildFailed(watchedChildren)
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // The server is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`Timed out waiting for ${url}`)
}

const initializeAdminCredential = async (backendBaseURL, initialPassword) => {
  const backendOrigin = new URL(backendBaseURL).origin
  const initialCSRFResponse = await fetch(`${backendBaseURL}api/csrf`, {
    headers: { 'X-Requested-With': 'XMLHttpRequest' },
  })
  const initialCSRFBody = await initialCSRFResponse.json().catch(() => ({ success: false }))
  const initialCSRF = initialCSRFBody.obj?.token
  let sessionCookie = initialCSRFResponse.headers.get('set-cookie')?.split(';', 1)[0]
  if (!initialCSRFResponse.ok || initialCSRFBody.success !== true || typeof initialCSRF !== 'string' || !sessionCookie) {
    throw new Error(`initial admin CSRF setup failed (status ${initialCSRFResponse.status})`)
  }

  const loginResponse = await fetch(`${backendBaseURL}api/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      Cookie: sessionCookie,
      Origin: backendOrigin,
      'X-CSRF-Token': initialCSRF,
      'X-Requested-With': 'XMLHttpRequest',
    },
    body: new URLSearchParams({ user: 'admin', pass: initialPassword }),
  })
  const loginBody = await loginResponse.json().catch(() => ({ success: false }))
  if (!loginResponse.ok || loginBody.success !== true || loginBody.obj?.state !== 'password_reset') {
    throw new Error(`initial admin login did not enter the required password transition (status ${loginResponse.status})`)
  }
  sessionCookie = loginResponse.headers.get('set-cookie')?.split(';', 1)[0] || sessionCookie

  const csrfResponse = await fetch(`${backendBaseURL}api/csrf`, {
    headers: {
      Cookie: sessionCookie,
      'X-Requested-With': 'XMLHttpRequest',
    },
  })
  const csrfBody = await csrfResponse.json().catch(() => ({ success: false }))
  const csrf = csrfBody.obj?.token
  if (!csrfResponse.ok || csrfBody.success !== true || typeof csrf !== 'string') {
    throw new Error(`initial admin CSRF setup failed (status ${csrfResponse.status})`)
  }

  const password = `E2e-${randomBytes(24).toString('base64url')}aA1!`
  const transitionResponse = await fetch(`${backendBaseURL}api/v1/security/password/transition`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Cookie: sessionCookie,
      Origin: backendOrigin,
      'X-CSRF-Token': csrf,
      'X-Requested-With': 'XMLHttpRequest',
    },
    body: JSON.stringify({
      currentPassword: initialPassword,
      newUsername: 'admin',
      newPassword: password,
    }),
  })
  const transitionBody = await transitionResponse.json().catch(() => ({ success: false }))
  if (!transitionResponse.ok || transitionBody.success !== true || transitionBody.obj?.state !== 'authenticated') {
    throw new Error(`initial admin password transition failed (status ${transitionResponse.status})`)
  }
  return password
}

const stopAll = () => {
  if (stopped) return
  stopped = true

  fs.rmSync(stateTempPath, { force: true })
  fs.rmSync(statePath, { force: true })
  for (const child of [...children].reverse()) {
    if (!child.pid || child.exitCode !== null) continue
    if (process.platform === 'win32') {
      spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], {
        stdio: 'ignore',
        windowsHide: true,
      })
      continue
    }
    try {
      process.kill(-child.pid, 'SIGTERM')
    } catch {
      // The process group may have already exited during test teardown.
    }
  }
}

process.on('SIGINT', () => {
  stopAll()
  process.exit(130)
})
process.on('SIGTERM', () => {
  stopAll()
  process.exit(143)
})
process.on('exit', stopAll)

const main = async () => {
  generateComponentOverlay()

  const backendEnv = {
    ...process.env,
    SUI_DB_FOLDER: dbDir,
    SUI_SECRET: randomBytes(32).toString('hex'),
    SUI_COOKIE_KEY: randomBytes(32).toString('base64'),
    SUI_LOG_LEVEL: 'warn',
    SUI_FORCE_COOKIE_SECURE: 'false',
    SUI_DISABLE_CORE: '1',
    SUI_E2E_WEB_PATH: e2eWebPath,
    SUI_E2E_BACKEND_LISTEN: '127.0.0.1:0',
    SUI_E2E_BACKEND_ADDRESS_FILE: backendAddressPath,
    SUI_COMPONENTS_INSTALLED_FILE: path.join(componentsStateDir, 'installed.json'),
    SUI_RUNTIME: runtimeDir,
    XUI_DISABLE_REMOTE: '1',
    APPDATA: appDataDir,
    LOCALAPPDATA: appDataDir,
    ZIG_GLOBAL_CACHE_DIR: zigGlobalCacheDir,
    ZIG_LOCAL_CACHE_DIR: zigLocalCacheDir,
    CGO_ENABLED: process.env.CGO_ENABLED || '1',
    ...(resolvedCC ? { CC: resolvedCC } : {}),
    GOTELEMETRY: 'off',
    GOTELEMETRYDIR: path.join(serverDir, 'go-telemetry'),
  }
  const backend = spawnLogged(
    'backend',
    'go',
    ['run', '-overlay', overlayPath, './tests/e2e/panel-server'],
    { cwd: repoRoot, env: backendEnv },
  )

  const backendWatch = [{ name: 'backend', child: backend }]
  const backendAddress = await waitForFile(backendAddressPath, readyTimeoutMs, backendWatch)
  if (!/^127\.0\.0\.1:\d+$/.test(backendAddress)) {
    throw new Error(`backend published an invalid address: ${backendAddress}`)
  }
  const backendOrigin = `http://${backendAddress}`
  const initialPassword = await waitForFile(path.join(dbDir, 'initial-admin.txt'), readyTimeoutMs, backendWatch)
  const backendBaseURL = `${backendOrigin}${e2eWebPath}`
  await waitForURL(`${backendBaseURL}login`, readyTimeoutMs, backendWatch)
  const password = await initializeAdminCredential(backendBaseURL, initialPassword)

  const viteCLI = path.join(frontendDir, 'node_modules', 'vite', 'bin', 'vite.js')
  const frontend = spawnLogged('frontend', process.execPath, [viteCLI, '--host', '127.0.0.1', '--port', '3000', '--strictPort'], {
    cwd: frontendDir,
    env: {
      ...process.env,
      SUI_E2E: '1',
      SUI_E2E_WEB_PATH: e2eWebPath,
      SUI_E2E_BACKEND_ORIGIN: backendOrigin,
    },
  })
  const watchedChildren = [...backendWatch, { name: 'frontend', child: frontend }]
  await waitForURL(`http://127.0.0.1:3000${e2eWebPath}login`, readyTimeoutMs, watchedChildren)

  fs.writeFileSync(stateTempPath, JSON.stringify({
    serverPid: process.pid,
    baseURL: `http://127.0.0.1:3000${e2eWebPath}`,
    backendURL: backendBaseURL,
    username: 'admin',
    password,
    dbDir,
  }, null, 2), { mode: 0o600 })
  fs.renameSync(stateTempPath, statePath)

  setInterval(() => {
    try {
      throwIfChildFailed(watchedChildren)
    } catch (error) {
      console.error(error)
      stopAll()
      process.exit(1)
    }
  }, 500)
}

main().catch((error) => {
  console.error(error)
  stopAll()
  process.exit(1)
})
