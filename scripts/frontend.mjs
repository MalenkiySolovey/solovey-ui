// Product invocation adapter: provenance is established before starting npm.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {productRoot} from './component-source-inputs.mjs'
import {prepareFrontendBuildInputs} from './frontend-build-inputs.mjs'

const tasks = new Set(['build', 'dev', 'typecheck', 'test', 'test:unit', 'test:e2e', 'lint', 'preview'])

export function npmCLI(env = process.env) {
  const nodeDirectory = path.dirname(process.execPath)
  const candidates = [env.npm_execpath, path.join(nodeDirectory, 'node_modules/npm/bin/npm-cli.js'), path.join(nodeDirectory, '../lib/node_modules/npm/bin/npm-cli.js')]
  const searchPath = Object.entries(env).find(([name]) => name.toLowerCase() === 'path')?.[1] || ''
  for (const directory of searchPath.split(path.delimiter).filter(Boolean)) {
    const executable = path.join(directory, 'npm')
    if (fs.existsSync(executable)) {
      const resolved = fs.realpathSync.native(executable)
      if (path.basename(resolved) === 'npm-cli.js') candidates.push(resolved)
    }
  }
  const cli = candidates.find(filename => filename && fs.existsSync(filename))
  assert.ok(cli, 'npm CLI unavailable for the selected Node toolchain')
  return fs.realpathSync.native(cli)
}

export function runFrontendTask(task, args = [], root = productRoot) {
  assert.ok(tasks.has(task), 'unsupported Product frontend task')
  root = fs.realpathSync.native(root)
  const input = prepareFrontendBuildInputs(root)
  const env = {...process.env, SOLOVEY_UI_INPUTS_SHA256: input.manifestSHA256,
    SUI_E2E_SERVER_SCRIPT: path.join(root, 'tests/e2e/run-server.js'),
    SUI_E2E_SERVER_CWD: root,
    SUI_E2E_IMPORT_FIXTURES_DIR: path.join(root, 'test-db'),
    SUI_E2E_RESULT_DIR: process.env.SUI_E2E_RESULT_DIR || path.join(root, 'tests/baseline/e2e'),
  }
  const result = spawnSync(process.execPath, [npmCLI(env), 'run', task, ...(args.length ? ['--', ...args] : [])], {cwd: path.join(root, 'frontend'), env, stdio: 'inherit', windowsHide: true})
  if (result.error) throw result.error
  return result.status ?? 1
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [task, ...args] = process.argv.slice(2)
  process.exitCode = runFrontendTask(task, args)
}
