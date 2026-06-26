import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

import {
  managedServerStatePath,
  readManagedServerPid,
  repositoryRoot,
  stopManagedServer,
} from './server-lifecycle'

const waitForManagedServer = async (timeoutMs: number): Promise<void> => {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (fs.existsSync(managedServerStatePath)) {
      try {
        const state = JSON.parse(fs.readFileSync(managedServerStatePath, 'utf8')) as { baseURL?: string }
        if (state.baseURL) {
          const response = await fetch(new URL('login', state.baseURL))
          if (response.status < 500) return
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
  fs.rmSync(managedServerStatePath, { force: true })

  const server = spawn(process.execPath, [path.join(repositoryRoot, 'tests', 'e2e', 'run-server.js')], {
    cwd: repositoryRoot,
    detached: true,
    env: process.env,
    stdio: 'ignore',
    windowsHide: true,
  })
  server.unref()

  try {
    await waitForManagedServer(180_000)
  } catch (error) {
    stopManagedServer(server.pid)
    throw error
  }
}
