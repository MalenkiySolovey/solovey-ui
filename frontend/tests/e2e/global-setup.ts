import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

import {
  cleanupManagedServerRuntime,
  managedServerDir,
  managedServerStatePath,
  readManagedServerPid,
  repositoryRoot,
  stopManagedServer,
} from './server-lifecycle'

const managedServerTimeoutMs = Number(process.env.SUI_E2E_READY_TIMEOUT_MS || (process.env.CI ? 900_000 : 360_000))

const readLogTail = (fileName: string): string => {
  const logPath = path.join(managedServerDir, fileName)
  if (!fs.existsSync(logPath)) return `${fileName}: <missing>`

  const lines = fs.readFileSync(logPath, 'utf8').trim().split(/\r?\n/)
  return `${fileName}:\n${lines.slice(-80).join('\n')}`
}

const withManagedServerLogs = (error: unknown): Error => {
  const message = error instanceof Error ? error.message : String(error)
  return new Error(`${message}\n\nManaged E2E server logs:\n${readLogTail('backend.log')}\n\n${readLogTail('frontend.log')}`)
}

const waitForManagedServer = async (timeoutMs: number, startupFailure: () => Error | undefined): Promise<void> => {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const failure = startupFailure()
    if (failure) throw failure
    if (fs.existsSync(managedServerStatePath)) {
      try {
        const state = JSON.parse(fs.readFileSync(managedServerStatePath, 'utf8')) as { baseURL?: string }
        if (state.baseURL) {
          const response = await fetch(new URL('login', state.baseURL))
          if (response.ok) return
        }
      } catch {
        // The state file or HTTP server is not ready yet.
      }
    }
    await new Promise(resolve => setTimeout(resolve, 500))
  }
  throw new Error(`Timed out waiting for managed E2E server state: ${managedServerStatePath}`)
}

export default async function globalSetup() {
  stopManagedServer(readManagedServerPid())
  cleanupManagedServerRuntime(false)

  const server = spawn(process.execPath, [path.join(repositoryRoot, 'tests', 'e2e', 'run-server.js')], {
    cwd: repositoryRoot,
    detached: true,
    env: process.env,
    stdio: ['ignore', 'inherit', 'inherit'],
    windowsHide: true,
  })
  let startupError: Error | undefined
  server.once('error', (error) => {
    startupError = error
  })
  server.once('exit', (code, signal) => {
    startupError ??= new Error(`Managed E2E server exited before readiness: code=${code} signal=${signal}`)
  })
  server.unref()

  try {
    await waitForManagedServer(managedServerTimeoutMs, () => startupError)
  } catch (error) {
    stopManagedServer(server.pid)
    cleanupManagedServerRuntime(true)
    throw withManagedServerLogs(error)
  }
}
