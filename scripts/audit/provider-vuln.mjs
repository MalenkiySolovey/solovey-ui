#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { resolveBuildPlan } from '../core-provider-build.mjs'

const root = fileURLToPath(new URL('../../', import.meta.url))
const baseTags = 'with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale'

export function auditProviders({ repoRoot = root, env = process.env, scan = spawnSync, log = console.log } = {}) {
  const catalog = JSON.parse(fs.readFileSync(path.join(repoRoot, 'core/provider/pins.json'), 'utf8'))
  let exitCode = 0
  for (const flavor of catalog.buildFlavors) {
    const plan = resolveBuildPlan({ provider: flavor.id, baseTags, repoRoot })
    log(`[govulncheck] provider=${plan.provider} modfile=${plan.modFile} tags=${plan.tags}`)
    const result = scan('govulncheck', ['-tags', plan.tags, './...'], {
      cwd: repoRoot,
      // Full-feature SSA analysis is large; keep a default GC budget for CI
      // while respecting a caller's explicit memory limit.
      env: { ...env, GOMEMLIMIT: env.GOMEMLIMIT || '4GiB', GOFLAGS: `-mod=readonly -modfile=${plan.modFile}` },
      stdio: 'inherit',
      windowsHide: true,
    })
    if (result.error || result.signal || !Number.isInteger(result.status)) {
      log(`[govulncheck] ${plan.provider}: scanner failed: ${result.error?.message ?? result.signal ?? 'no exit status'}`)
      exitCode ||= 1
    } else {
      exitCode ||= result.status
    }
  }
  return exitCode
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    process.exitCode = auditProviders()
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
