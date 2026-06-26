const fs = require('node:fs')
const path = require('node:path')
const { spawn, spawnSync } = require('node:child_process')

const repoRoot = path.resolve(__dirname, '..', '..')
const frontendDir = path.join(repoRoot, 'frontend')
const phaseDir = path.join(repoRoot, 'tests', 'baseline', 'phase6')
const serverDir = path.join(phaseDir, 'e2e-server')
const dbDir = path.join(phaseDir, 'e2e-db')
const appDataDir = path.join(phaseDir, 'appdata')
const tempDir = path.join(phaseDir, 'tmp')
const zigGlobalCacheDir = path.join(phaseDir, 'zig-global-cache')
const zigLocalCacheDir = path.join(phaseDir, 'zig-local-cache')
const statePath = path.join(serverDir, 'state.json')
const bundledZig = path.join(repoRoot, '..', '..', '.devtools', 'zig-x86_64-windows-0.16.0', 'zig.exe')
const resolvedCC = process.env.CC || (process.platform === 'win32' && fs.existsSync(bundledZig) ? `${bundledZig} cc` : undefined)
const readyTimeoutMs = Number(process.env.SUI_E2E_READY_TIMEOUT_MS || 300000)

fs.mkdirSync(serverDir, { recursive: true })
fs.mkdirSync(appDataDir, { recursive: true })
fs.mkdirSync(tempDir, { recursive: true })
fs.mkdirSync(zigGlobalCacheDir, { recursive: true })
fs.mkdirSync(zigLocalCacheDir, { recursive: true })
fs.rmSync(statePath, { force: true })
fs.rmSync(dbDir, { recursive: true, force: true })
fs.mkdirSync(dbDir, { recursive: true })

const logStream = (name) => fs.createWriteStream(path.join(serverDir, `${name}.log`), { flags: 'a' })

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

const rememberChildFailure = (child, name, error) => {
  const failure = error instanceof Error ? error : new Error(String(error))
  childFailures.set(child, failure)
  return `[${name}] ${failure.stack || failure.message}\n`
}

const throwIfChildFailed = (watchedChildren) => {
  for (const { name, child } of watchedChildren) {
    const failure = childFailures.get(child)
    if (failure) {
      throw new Error(`${name} failed before the E2E server became ready: ${failure.message}`)
    }
  }
}

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
    if (code && code !== 0) {
      const message = rememberChildFailure(child, name, new Error(`exited code=${code} signal=${signal}`))
      process.stderr.write(message)
      log.write(message)
    }
  })
  return child
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
      if (response.status < 500) return
    } catch {
      // server is still starting
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`Timed out waiting for ${url}`)
}

let stopped = false
const stopAll = () => {
  if (stopped) return
  stopped = true

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
  const backendEnv = {
    ...process.env,
    SUI_DB_FOLDER: dbDir,
    SUI_SECRET: 'phase6-e2e-secret',
    SUI_COOKIE_KEY: 'MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=',
    SUI_LOG_LEVEL: 'warn',
    SUI_FORCE_COOKIE_SECURE: 'false',
    SUI_DISABLE_CORE: '1',
    XUI_DISABLE_REMOTE: '1',
    APPDATA: appDataDir,
    LOCALAPPDATA: appDataDir,
    TEMP: tempDir,
    TMP: tempDir,
    ZIG_GLOBAL_CACHE_DIR: zigGlobalCacheDir,
    ZIG_LOCAL_CACHE_DIR: zigLocalCacheDir,
    CGO_ENABLED: process.env.CGO_ENABLED || '1',
    ...(resolvedCC ? { CC: resolvedCC } : {}),
    GOTELEMETRY: 'off',
    GOTELEMETRYDIR: path.join(serverDir, 'go-telemetry'),
  }
  const backend = spawnLogged('backend', 'go', ['run', './tests/e2e/panel-server'], { cwd: repoRoot, env: backendEnv })

  const backendWatch = [{ name: 'backend', child: backend }]
  const password = await waitForFile(path.join(dbDir, 'initial-admin.txt'), readyTimeoutMs, backendWatch)
  await waitForURL('http://127.0.0.1:2095/app/login', readyTimeoutMs, backendWatch)

  const viteCLI = path.join(frontendDir, 'node_modules', 'vite', 'bin', 'vite.js')
  const frontend = spawnLogged('frontend', process.execPath, [viteCLI, '--host', '127.0.0.1', '--port', '3000', '--strictPort'], {
    cwd: frontendDir,
    env: {
      ...process.env,
      SUI_E2E: '1',
    },
  })
  const frontendWatch = [...backendWatch, { name: 'frontend', child: frontend }]
  await waitForURL('http://127.0.0.1:3000/app/login', readyTimeoutMs, frontendWatch)
  for (const modulePath of ['Home.vue', 'MigrateXui.vue', 'Settings.vue', 'Audit.vue']) {
    await waitForURL(`http://127.0.0.1:3000/src/views/${modulePath}`, readyTimeoutMs, frontendWatch)
  }

  fs.writeFileSync(statePath, JSON.stringify({
    serverPid: process.pid,
    baseURL: 'http://127.0.0.1:3000/app/',
    backendURL: 'http://127.0.0.1:2095/app/',
    username: 'admin',
    password,
    dbDir,
  }, null, 2))

  setInterval(() => {}, 2147483647)
}

main().catch((error) => {
  console.error(error)
  stopAll()
  process.exit(1)
})
