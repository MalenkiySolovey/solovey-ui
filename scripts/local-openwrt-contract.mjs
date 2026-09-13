#!/usr/bin/env node

import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

try {
  const scriptRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const sourceRoot = path.resolve(argumentValue('--source-root') ?? scriptRoot)
  const distro = argumentValue('--wsl-distro') ?? 'openwrt-builder'
  const linuxGo = argumentValue('--linux-go') ?? '/home/sui-builder/openwrt/test-go-1.26.6/go/bin/go'
  const skipBrowser = process.argv.includes('--skip-browser')
  const frontendRoot = path.join(sourceRoot, 'frontend')
  const workspaceTools = path.resolve(sourceRoot, '..', '..', 'tools')
  const workspaceRoot = path.resolve(sourceRoot, '..', '..')
  const goRoot = path.join(workspaceRoot, '.devtools', 'go')
  const windowsPath = process.env.Path ?? process.env.PATH ?? ''

  requireFile(path.join(sourceRoot, 'go.mod'))
  requireFile(path.join(frontendRoot, 'package-lock.json'))
  const linuxSource = windowsPathToWSL(sourceRoot)

  step('Layer B pinned OpenWrt source capability contract', process.execPath, [
    path.join(sourceRoot, 'scripts', 'openwrt-listener-capability-contract.mjs'),
    '--source-root', sourceRoot,
  ], sourceRoot)
  step('Layers A/B/C/D/E/F Linux production-composition contracts', 'wsl.exe', [
    '-d', distro, '--', 'env',
    `SOLOVEY_GO_PROGRAM=${linuxGo}`,
    'GOCACHE=/home/sui-builder/.cache/solovey-r8-go-build',
    'GOMODCACHE=/home/sui-builder/.cache/solovey-r8-go-mod',
    'GOPATH=/home/sui-builder/.cache/solovey-r8-gopath',
    'bash', `${linuxSource}/scripts/native-linux-contract.sh`, '--source-root', linuxSource,
  ], sourceRoot)
  step('Layer F frontend API wire unit contract', process.execPath, [
    'node_modules/vitest/vitest.mjs', 'run', '../components/server-protection/frontend/api.test.ts', '--config', 'vitest.config.ts',
  ], frontendRoot)

  if (!skipBrowser) {
    step('Layer F production browser no-TypeError contract', process.execPath, [
      'node_modules/@playwright/test/cli.js', 'test', 'panel-operability.spec.ts', '--config', 'playwright.config.ts',
    ], frontendRoot, {
      Path: `${path.join(goRoot, 'bin')}${path.delimiter}${workspaceTools}${path.delimiter}${windowsPath}`,
      SUI_E2E_GO_PROGRAM: path.join(goRoot, 'bin', 'go.exe'),
      GOMODCACHE: path.join(workspaceRoot, '.devtools', 'gomodcache'),
      GOCACHE: path.join(workspaceRoot, '.devtools', 'gocache'),
      GOENV: 'off',
      GOTOOLCHAIN: 'auto',
      GOWORK: 'off',
    })
  } else {
    console.log('[local-openwrt-contract] browser contract SKIPPED; result is not an acceptance PASS')
  }

  console.log(skipBrowser
    ? '[local-openwrt-contract] PARTIAL (browser evidence omitted)'
    : '[local-openwrt-contract] PASS HOST_TESTED_WSL + SOURCE_VERIFIED; not physical OpenWrt evidence')
} catch (error) {
  console.error(`[local-openwrt-contract] ERROR: ${error instanceof Error ? error.message : String(error)}`)
  process.exit(1)
}

function argumentValue(name) {
  const index = process.argv.indexOf(name)
  if (index === -1) return undefined
  const value = process.argv[index + 1]
  if (!value || value.startsWith('--')) throw new Error(`${name} requires a value`)
  return value
}

function requireFile(file) {
  if (!fs.statSync(file).isFile()) throw new Error(`required source file is unavailable: ${file}`)
}

function step(label, command, args, cwd, extraEnv = {}) {
  console.log(`[local-openwrt-contract] ${label}`)
  const env = { ...process.env, ...extraEnv }
  if (Object.hasOwn(extraEnv, 'Path')) {
    for (const key of Object.keys(env)) {
      if (key.toLowerCase() === 'path' && key !== 'Path') delete env[key]
    }
  }
  const result = spawnSync(command, args, { cwd, env, stdio: 'inherit', windowsHide: true })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`${label} failed with exit code ${result.status ?? 'unknown'}`)
}

function windowsPathToWSL(value) {
  const match = path.resolve(value).match(/^([A-Za-z]):[\\/](.*)$/)
  if (!match) throw new Error(`source root is not a Windows drive path: ${value}`)
  return `/mnt/${match[1].toLowerCase()}/${match[2].replaceAll('\\', '/')}`
}
