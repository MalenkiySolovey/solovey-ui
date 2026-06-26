import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

export const repositoryRoot = path.resolve(process.cwd(), '..')
export const managedServerStatePath = path.join(
  repositoryRoot,
  'tests',
  'baseline',
  'phase6',
  'e2e-server',
  'state.json',
)

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

  if (process.platform === 'win32') {
    spawnSync('taskkill', ['/pid', String(pid), '/T', '/F'], {
      stdio: 'ignore',
      windowsHide: true,
    })
    return
  }

  try {
    process.kill(pid, 'SIGTERM')
  } catch {
    // The managed server may already have stopped after a setup or test failure.
  }
}
