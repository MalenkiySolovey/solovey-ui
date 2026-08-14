import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

export const repositoryRoot = path.resolve(process.cwd(), '..')
export const managedE2EResultDir = path.join(
  repositoryRoot,
  'tests',
  'baseline',
  'e2e',
)
export const managedServerDir = path.join(
  managedE2EResultDir,
  'e2e-server',
)
export const managedServerStatePath = path.join(
  managedServerDir,
  'state.json',
)

const managedServerEntryPoint = path.join(repositoryRoot, 'tests', 'e2e', 'run-server.js')
const managedRuntimePaths = [
  path.join(managedE2EResultDir, 'e2e-db'),
  path.join(managedE2EResultDir, '.runtime'),
  path.join(managedE2EResultDir, 'appdata'),
  path.join(managedE2EResultDir, 'zig-global-cache'),
  path.join(managedE2EResultDir, 'zig-local-cache'),
  path.join(managedE2EResultDir, 'components'),
]

export const readManagedServerPid = (): number | undefined => {
  if (!fs.existsSync(managedServerStatePath)) return undefined

  try {
    const state = JSON.parse(fs.readFileSync(managedServerStatePath, 'utf8')) as { serverPid?: number }
    return Number.isInteger(state.serverPid) && Number(state.serverPid) > 0
      ? Number(state.serverPid)
      : undefined
  } catch {
    return undefined
  }
}

export const stopManagedServer = (pid: number | undefined): void => {
  if (!pid) return

  const commandLine = readProcessCommandLine(pid)
  if (!commandLine || !normalizeCommandLine(commandLine).includes(normalizeCommandLine(managedServerEntryPoint))) {
    return
  }

  if (process.platform === 'win32') {
    spawnSync('taskkill', ['/pid', String(pid), '/T', '/F'], {
      stdio: 'ignore',
      windowsHide: true,
    })
  } else {
    try {
      process.kill(pid, 'SIGTERM')
    } catch {
      // The managed server may already have stopped after a setup or test failure.
      return
    }
  }

  const waitBuffer = new Int32Array(new SharedArrayBuffer(4))
  const deadline = Date.now() + 10_000
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0)
      Atomics.wait(waitBuffer, 0, 0, 100)
    } catch {
      return
    }
  }
}

export const cleanupManagedServerRuntime = (preserveLogs: boolean): void => {
  const failures: Error[] = []
  for (const runtimePath of managedRuntimePaths) {
    removeRuntimePath(runtimePath, failures)
  }

  if (!preserveLogs) {
    removeRuntimePath(managedServerDir, failures)
  } else {
    for (const runtimePath of [
      managedServerStatePath,
      `${managedServerStatePath}.tmp`,
      path.join(managedServerDir, 'generated'),
      path.join(managedServerDir, 'go-overlay.json'),
      path.join(managedServerDir, 'backend-address.txt'),
      path.join(managedServerDir, 'backend-address.txt.tmp'),
      path.join(managedServerDir, 'go-telemetry'),
    ]) {
      removeRuntimePath(runtimePath, failures)
    }
  }

  if (failures.length > 0) {
    throw new Error(`Managed E2E runtime cleanup failed:\n${failures.map(error => error.message).join('\n')}`)
  }
}

const removeRuntimePath = (runtimePath: string, failures: Error[]): void => {
  try {
    fs.rmSync(runtimePath, { recursive: true, force: true, maxRetries: 50, retryDelay: 200 })
  } catch (error) {
    const cause = error instanceof Error ? error : new Error(String(error))
    failures.push(new Error(`Unable to remove managed runtime path ${runtimePath}: ${cause.message}`, { cause }))
  }
}

const normalizeCommandLine = (value: string): string => value.replaceAll('\\', '/').toLowerCase()

const readProcessCommandLine = (pid: number): string => {
  if (process.platform === 'win32') {
    const result = spawnSync('powershell.exe', [
      '-NoProfile',
      '-Command',
      `(Get-CimInstance Win32_Process -Filter 'ProcessId = ${pid}').CommandLine`,
    ], { encoding: 'utf8', windowsHide: true })
    return result.status === 0 ? result.stdout.trim() : ''
  }

  const procCommandLine = `/proc/${pid}/cmdline`
  if (fs.existsSync(procCommandLine)) {
    return fs.readFileSync(procCommandLine, 'utf8').replaceAll('\0', ' ')
  }

  const result = spawnSync('ps', ['-p', String(pid), '-o', 'command='], { encoding: 'utf8' })
  return result.status === 0 ? result.stdout.trim() : ''
}
