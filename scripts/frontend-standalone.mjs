// Product-side qualification: the copied frontend only receives its own source,
// locked npm dependencies, and the explicitly pinned UI projection.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'
import {execFileSync, spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {stripVTControlCharacters} from 'node:util'
import {productRoot, digest} from './component-source-inputs.mjs'
import {prepareFrontendBuildInputs} from './frontend-build-inputs.mjs'
import {npmCLI} from './frontend.mjs'
import {verifyDist, publishAssets} from './frontend-assets.mjs'

export function qualifyStandaloneFrontend({root = productRoot, out, previousDist} = {}) {
  root = fs.realpathSync.native(root)
  out = path.resolve(out || path.join(root, '.artifacts/frontend-standalone'))
  fs.mkdirSync(out, {recursive: true})
  assert.ok(!fs.existsSync(path.join(out, 'full')), 'qualification output already contains a build; select a fresh output directory')
  const input = prepareFrontendBuildInputs(root)
  const temporary = fs.realpathSync.native(fs.mkdtempSync(path.join(os.tmpdir(), 'solovey standalone ')))
  const frontend = path.join(temporary, 'frontend')
  fs.mkdirSync(frontend)
  const sourceFiles = execFileSync('git', ['ls-files', '-z', '--', 'frontend'], {cwd: root, encoding: 'utf8'}).split('\0').filter(Boolean)
  assert.ok(sourceFiles.length > 0)
  const sourceHashes = {}
  for (const name of sourceFiles) {
    const destination = path.join(temporary, name)
    fs.mkdirSync(path.dirname(destination), {recursive: true})
    fs.copyFileSync(path.join(root, name), destination)
    sourceHashes[name] = digest(fs.readFileSync(destination))
  }
  const env = {...process.env, SOLOVEY_UI_INPUTS_SHA256: input.manifestSHA256, CI: 'true'}
  for (const key of Object.keys(env)) if (key.toLowerCase() === 'path') delete env[key]
  env.PATH = [path.dirname(process.execPath), ...(process.platform === 'win32' ? [path.join(process.env.SystemRoot, 'System32')] : [])].join(path.delimiter)
  // npm needs a shell, but PATH must still exclude Go and Git.
  if (process.platform !== 'win32') env.npm_config_script_shell = '/bin/sh'
  env.SOLOVEY_UI_PROFILE = 'full'
  delete env.SOLOVEY_UI_COMPONENT_IDS
  const npm = npmCLI()
  const report = {schema: 'solovey.frontend-standalone-proof/v1', observedAt: new Date().toISOString(), productSHA: execFileSync('git', ['rev-parse', 'HEAD'], {cwd: root, encoding: 'utf8'}).trim(), sourceSHA256: digest(JSON.stringify(sourceHashes)), sourceHashes, node: process.version, platform: process.platform, input, isolation: {frontend, siblings: [], goUnavailable: false, gitUnavailable: false}, commands: [], profiles: {}}
  const run = (name, args, expected = 0, suppliedEnv = env) => {
    const result = spawnSync(process.execPath, [npm, ...args], {cwd: frontend, env: suppliedEnv, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, windowsHide: true})
    if (result.error) throw result.error
    fs.writeFileSync(path.join(out, name + '.log'), result.stdout + result.stderr)
    report.commands.push({name, args, status: result.status, expected})
    assert.ok(expected === 0 ? result.status === 0 : result.status !== 0, name + ': ' + (result.stdout + result.stderr).slice(-8000))
    console.log('[frontend-standalone] ' + name + ': ' + (expected === 0 ? 'PASS' : 'rejected'))
    return stripVTControlCharacters(result.stdout + result.stderr)
  }
  const qualifyAssets = (profile, directory) => {
    const manifestBytes = fs.readFileSync(path.join(directory, '.vite/manifest.json'))
    const manifest = JSON.parse(manifestBytes)
    const entries = Object.keys(manifest).filter(key => key.startsWith('.component-inputs/components/') && key.endsWith('/frontend/index.ts')).sort()
    const files = [...verifyDist(directory).keys()].sort()
    report.profiles[profile] = {manifestSHA256: digest(manifestBytes), components: entries, files}
    const check = spawnSync(process.execPath, [path.join(root, 'scripts/check-frontend-profile.mjs'), '--profile', profile === 'full' ? 'full' : 'core', '--dist', directory], {cwd: root, encoding: 'utf8', windowsHide: true})
    assert.equal(check.status, 0, check.stdout + check.stderr)
    assert.equal(entries.length, profile === 'full' ? input.components.length : 0)
  }
  try {
    for (const program of ['go', 'git']) {
      const result = spawnSync(program, ['version'], {cwd: frontend, env, encoding: 'utf8', windowsHide: true})
      assert.equal(result.error?.code, 'ENOENT', program + ' must be unavailable inside the independent build')
      report.isolation[program + 'Unavailable'] = true
    }
    assert.deepEqual(fs.readdirSync(temporary), ['frontend'])
    run('npm-ci', ['ci', '--prefer-offline', '--no-audit', '--no-fund'])
    assert.match(run('missing-inputs', ['run', 'build'], 1), /ENOENT/)
    fs.cpSync(input.directory, path.join(frontend, '.component-inputs'), {recursive: true})
    const noHash = {...env}; delete noHash.SOLOVEY_UI_INPUTS_SHA256
    assert.match(run('missing-hash', ['run', 'build'], 1, noHash), /SHA256|ERR_ASSERTION/)
    assert.match(run('wrong-hash', ['run', 'build'], 1, {...env, SOLOVEY_UI_INPUTS_SHA256: '0'.repeat(64)}), /manifest SHA256 mismatch/)
    for (const task of ['typecheck', 'lint', 'test']) run(task, ['run', task])
    for (const profile of ['full', 'core', 'minimal']) {
      run('build-' + profile, ['run', 'build'], 0, {...env, SOLOVEY_UI_PROFILE: profile})
      const directory = path.join(out, profile)
      fs.cpSync(path.join(frontend, 'dist'), directory, {recursive: true})
      qualifyAssets(profile, directory)
    }
    assert.equal(report.profiles.core.manifestSHA256, report.profiles.minimal.manifestSHA256)
    const saved = path.join(temporary, 'declared-full-inputs'), supplied = path.join(frontend, '.component-inputs')
    fs.renameSync(supplied, saved)
    try {
      const manifest = JSON.parse(fs.readFileSync(path.join(saved, 'manifest.json')))
      manifest.components = []
      const bytes = JSON.stringify(manifest, null, 2) + '\n'
      fs.mkdirSync(path.join(supplied, 'components'), {recursive: true})
      fs.writeFileSync(path.join(supplied, 'manifest.json'), bytes)
      run('build-core-empty-inputs', ['run', 'build'], 0, {...env, SOLOVEY_UI_PROFILE: 'core', SOLOVEY_UI_INPUTS_SHA256: digest(bytes)})
      const directory = path.join(out, 'core-empty-inputs')
      fs.cpSync(path.join(frontend, 'dist'), directory, {recursive: true})
      qualifyAssets('core-empty-inputs', directory)
      assert.equal(report.profiles.core.manifestSHA256, report.profiles['core-empty-inputs'].manifestSHA256)
    } finally {
      assert.equal(path.dirname(supplied), frontend)
      fs.rmSync(supplied, {recursive: true, force: true})
      fs.renameSync(saved, supplied)
    }
    const target = path.join(out, 'rollback/web/html')
    const generations = previousDist ? [path.resolve(previousDist), path.join(out, 'full'), path.resolve(previousDist), path.join(out, 'full')] : [path.join(out, 'core'), path.join(out, 'full'), path.join(out, 'core'), path.join(out, 'full')]
    report.rollback = []
    for (const generation of generations) {
      publishAssets(generation, target)
      const sha256 = digest(fs.readFileSync(path.join(target, '.vite/manifest.json')))
      assert.equal(sha256, digest(fs.readFileSync(path.join(generation, '.vite/manifest.json'))))
      report.rollback.push({generation, manifestSHA256: sha256, files: verifyDist(target).size})
    }
    report.result = 'PASS'
    fs.writeFileSync(path.join(out, 'proof.json'), JSON.stringify(report, null, 2) + '\n')
    return report
  } finally {
    assert.equal(path.dirname(temporary), fs.realpathSync.native(os.tmpdir()))
    assert.ok(path.basename(temporary).startsWith('solovey standalone '))
    fs.rmSync(temporary, {recursive: true, force: true})
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = new Map()
  for (let index = 2; index < process.argv.length; index += 2) {
    assert.ok(['--out', '--previous-dist'].includes(process.argv[index]) && process.argv[index + 1], 'invalid standalone qualification arguments')
    args.set(process.argv[index], process.argv[index + 1])
  }
  const report = qualifyStandaloneFrontend({out: args.get('--out'), previousDist: args.get('--previous-dist')})
  console.log(JSON.stringify({result: report.result, sourceSHA256: report.sourceSHA256, profiles: Object.keys(report.profiles)}))
}
