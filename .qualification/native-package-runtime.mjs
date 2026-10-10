// Executes a packaged candidate in a private disposable fixture. Never prints
// credentials, child logs, seed DTOs or raw authenticated HTTP responses.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import net from 'node:net'
import dgram from 'node:dgram'
import crypto from 'node:crypto'
import {pathToFileURL} from 'node:url'
import { spawn, spawnSync } from 'node:child_process'

const flags = Object.fromEntries(process.argv.slice(2).reduce((all, arg, i, args) => {
  if (i % 2 === 0) all.push([arg.replace(/^--/, ''), args[i + 1]])
  return all
}, []))
if (!/^[a-f0-9]{40}$/.test(flags.commit || '') || !flags.helper || !flags.output || (!!flags.binary === !!flags.image)) {
  throw new Error('RUNTIME_ARGUMENTS_INVALID')
}
const hash = (file) => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-w5-package-'))
const data = path.join(temporary, 'data')
fs.mkdirSync(data, { mode: 0o700 })
fs.writeFileSync(path.join(data, '.w5-disposable'), 'W5_SYNTHETIC_ONLY\n', { mode: 0o600 })
const secrets = [crypto.randomBytes(32).toString('base64url'), crypto.randomBytes(32).toString('base64url'),
  crypto.randomBytes(32).toString('base64url'), crypto.randomBytes(32).toString('base64url'), crypto.randomBytes(32).toString('base64url')]
const environment = { ...Object.fromEntries(Object.entries(process.env).filter(([name]) => !name.startsWith('SUI_') && !name.startsWith('SOLOVEY_'))),
  SUI_DB_FOLDER: data, SUI_CACHE_FOLDER: data, SUI_STAGING_FOLDER: data, SUI_RUNTIME: path.join(temporary, 'runtime'),
  APPDATA: path.join(temporary, 'profile'), LOCALAPPDATA: path.join(temporary, 'profile') }
const report = { test: 'packaged-native-runtime', timestamp: new Date().toISOString(), commit: flags.commit,
  environment: { platform: process.platform, architecture: process.arch, release: os.release(), runner: process.env.RUNNER_NAME || 'local' },
  evidenceClass: 'HOST_NATIVE_PROVEN', assertions: [], status: 'RUNNING', candidate: {}, cleanup: 'PENDING' }
report.fixtureSource = { commit: flags['fixture-commit'] || null, helperSha256: hash(path.resolve(flags.helper)),
  driverSha256: hash(new URL(import.meta.url)) }
let child, container, witness, secretLeak = false, tail = '', boundedLog = ''
const assertion = (name, condition) => { if (!condition) throw new Error(name); report.assertions.push(name) }
const invoke = (program, args, options = {}) => {
  const result = spawnSync(program, args, { encoding: 'utf8', timeout: 120000, windowsHide: true, ...options })
  if (result.error || result.status !== 0) throw new Error(`EXECUTION_FAILED_${path.basename(program)}_${result.status ?? 'spawn'}`)
  return result.stdout
}
const helper = (mode, input) => {
  const result = spawnSync(path.resolve(flags.helper), [mode], { input: JSON.stringify(input), env: environment,
    encoding: 'utf8', timeout: 120000, windowsHide: true })
  if (result.error || result.status !== 0) {
    const stage = result.stderr?.trim().split(/\r?\n/).at(-1)
    throw new Error(stage && /^[A-Za-z0-9_:.-]{1,180}$/.test(stage) ? stage : `FIXTURE_${mode}_FAILED`)
  }
  return JSON.parse(result.stdout.trim().split(/\r?\n/).at(-1))
}
const freePort = async (protocol) => {
  if (protocol === 'udp') {
    const socket = dgram.createSocket('udp4')
    await new Promise((resolve, reject) => { socket.once('error', reject); socket.bind(0, '127.0.0.1', resolve) })
    const port = socket.address().port
    await new Promise((resolve) => socket.close(resolve))
    return port
  }
  const socket = net.createServer()
  await new Promise((resolve, reject) => { socket.once('error', reject); socket.listen(0, '127.0.0.1', resolve) })
  const port = socket.address().port
  await new Promise((resolve) => socket.close(resolve))
  return port
}
const waitReady = async (url) => {
  const deadline = Date.now() + 45000
  while (Date.now() < deadline) {
    if (child && child.exitCode !== null) throw new Error('PACKAGED_PROCESS_EXITED_BEFORE_READY')
    try { const r = await fetch(url, { signal: AbortSignal.timeout(1500) }); await r.body?.cancel(); if (r.status === 200) return } catch {}
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  throw new Error('PACKAGED_HTTP_READY_TIMEOUT')
}
const inspectLogs = (chunk) => {
  const text = tail + chunk.toString('utf8')
  if (secrets.some((secret) => text.includes(secret)) || text.includes('-----BEGIN PRIVATE KEY-----')) secretLeak = true
  tail = text.slice(-256)
  boundedLog = (boundedLog + chunk.toString('utf8')).slice(-65536)
}
const sanitizedDiagnostics = () => {
  let text = boundedLog
  for (const secret of secrets) text = text.split(secret).join('[REDACTED]')
  text = text.replace(/-----BEGIN [^-]+-----[\s\S]*?-----END [^-]+-----/g, '[REDACTED_PEM]')
  text = text.replace(/(password|token|secret|credential|cookie|authorization)(\s*[:=]\s*)[^\s,}]+/gi, '$1$2[REDACTED]')
  return text.split(/\r?\n/).filter((line) => !/initial.admin|BEGIN|END.*KEY|^\s*[A-Za-z0-9+/=]{32,}\s*$|Authorization:\s|Cookie:\s/i.test(line)).slice(-70).join('\n')
}
const captureContainerLogs = () => {
  if (!container) return
  const result = spawnSync('docker', ['logs', '--tail', '100', container], { encoding: 'utf8', timeout: 10000 })
  inspectLogs(Buffer.from((result.stdout || '') + (result.stderr || '')))
}

const startUDPWitness = async () => {
  const program = path.resolve(flags['udp-witness'])
  assertion('udp_witness_is_regular_goal_fixture', fs.statSync(program).isFile())
  report.fixtureSource.udpWitnessSha256 = hash(program)
  witness = flags.image
    ? spawn('docker', ['exec', container, '/opt/w5/udp-witness'], {windowsHide: true, stdio: ['ignore', 'pipe', 'pipe']})
    : spawn(program, [], {windowsHide: true, env: environment, stdio: ['ignore', 'pipe', 'pipe']})
  const port = await new Promise((resolve, reject) => {
    let text = ''
    const timer = setTimeout(() => reject(new Error('UDP_WITNESS_READY_TIMEOUT')), 5000)
    witness.once('error', () => {clearTimeout(timer); reject(new Error('UDP_WITNESS_START_FAILED'))})
    witness.once('exit', () => {clearTimeout(timer); reject(new Error('UDP_WITNESS_EXITED'))})
    witness.stdout.on('data', chunk => {
      text += chunk.toString('utf8')
      if (text.length > 1024) {clearTimeout(timer); reject(new Error('UDP_WITNESS_FRAME_INVALID')); return}
      if (!text.includes('\n')) return
      try {
        const frame = JSON.parse(text.split('\n')[0])
        if (frame.loopback !== true || !Number.isInteger(frame.port) || frame.port < 1 || frame.port > 65535) throw new Error()
        clearTimeout(timer); resolve(frame.port)
      } catch {clearTimeout(timer); reject(new Error('UDP_WITNESS_FRAME_INVALID'))}
    })
    witness.stderr.resume()
  })
  report.udpWitness = {loopbackOnly: true, port, maxLifetimeMs: 120000, namespace: flags.image ? 'candidate-container' : 'candidate-native-host'}
  return port
}

async function run() {
  const docker = !!flags.image
  const ports = Object.fromEntries(await Promise.all(['hysteria', 'hysteria2', 'tuic'].map(async (kind) => [kind, await freePort('udp')])))
  const webPort = await freePort('tcp'), subPort = await freePort('tcp')
  const input = { Root: data, Listen: docker ? '0.0.0.0' : '127.0.0.1', WebPort: webPort, SubPort: subPort,
    Ports: ports, WriteToken: secrets[0], ReadToken: secrets[1], Passwords: secrets.slice(2, 4), IncludeNaive: true }
  if (flags['browser-module']) { input.AdminUser = 'qualification'; input.AdminPassword = secrets[4] }
  const seed = helper('seed', input)
  const database = path.join(data, 'solovey-ui.db')
  const seededHash = hash(database)
  report.fixtureSeedSha256 = seededHash
  if (!docker) {
    const binary = path.resolve(flags.binary)
    report.candidate = { sha256: hash(binary), binary: path.basename(binary) }
    const version = invoke(binary, ['-v'], { env: environment })
    assertion('official_core_1_14_2_version', /Sing-Box\s+v1\.14\.2(?:\s|$)/.test(version))
    assertion('public_product_version_unchanged', /Solovey UI Panel\s+2026\.3\.3(?:\s|$)/.test(version))
    const beforeNames = fs.readdirSync(data).sort().join('\n')
    const doctorStarted = Date.now()
    const health = spawnSync(binary, ['doctor', '--panel'], { encoding: 'utf8', env: environment, timeout: 30000, windowsHide: true })
    report.offlineDoctor = { exitCode: health.status, databaseUnchanged: hash(database) === seededHash,
      directoryEntriesUnchanged: beforeNames === fs.readdirSync(data).sort().join('\n'), timedOut: health.error?.code === 'ETIMEDOUT',
      processWallMs: Date.now() - doctorStarted, configuredProbeDeadlineMs: 5000 }
    const originalEntries = new Set(beforeNames.split('\n'))
    report.offlineDoctor.newEntries = fs.readdirSync(data).filter((name) => !originalEntries.has(name))
    report.offlineDoctor.onlySQLiteCoordinationSidecars = report.offlineDoctor.newEntries.every((name) => ['solovey-ui.db-wal', 'solovey-ui.db-shm'].includes(name))
    inspectLogs(Buffer.from((health.stdout || '') + (health.stderr || '')))
    assertion('offline_doctor_preserves_database_bytes_no_migration', report.offlineDoctor.databaseUnchanged && report.offlineDoctor.onlySQLiteCoordinationSidecars)
    assertion('offline_doctor_reports_unavailable_panel', health.status === 1)
    child = spawn(binary, [], { cwd: path.dirname(binary), env: environment, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
    child.stdout.on('data', inspectLogs); child.stderr.on('data', inspectLogs)
    child.on('error', () => { report.processSpawnFailed = true })
    await waitReady(`http://127.0.0.1:${webPort}/app/login`)
    if (process.platform === 'win32') {
      const program = path.join(process.env.SystemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe')
      const text = invoke(program, ['-NoProfile', '-NonInteractive', '-Command',
        `$ErrorActionPreference='Stop'; @(Get-NetTCPConnection -OwningProcess ${child.pid} -State Listen | Select-Object LocalAddress,LocalPort) | ConvertTo-Json -Compress`])
      const listeners = JSON.parse(text)
      const list = Array.isArray(listeners) ? listeners : [listeners]
      assertion('candidate_tcp_listeners_loopback_only', list.length >= 2 && list.every((listener) => ['127.0.0.1', '::1'].includes(listener.LocalAddress)))
      report.listenerCount = list.length
      if (flags['private-api-check'] === 'true') {
        const privateListeners = list.filter((listener) => ![webPort, subPort].includes(listener.LocalPort))
        assertion('single_private_tcp_listener_identified_from_owned_process', privateListeners.length === 1 && privateListeners[0].LocalAddress === '127.0.0.1')
        const privateProbe = helper('private-probe', { PrivatePort: privateListeners[0].LocalPort })
        report.assertions.push(...privateProbe.assertions)
      }
      const library = path.join(path.dirname(binary), 'libcronet.dll')
      const expected = { x64: '3217c6260fbca5f16072e0b79735742f40109a63bb0ff88fd6b96dd6b54a2928', arm64: 'a75a1b99a7e31802cf67ee14763e5cb9db6a24aaf7062f86c49f2fc1c030fcf0' }[process.arch]
      assertion('module_authenticated_cronet_dll_hash', fs.existsSync(library) && hash(library) === expected)
      report.cronetLibrarySha256 = expected
    }
    input.API = `http://127.0.0.1:${webPort}/app/`
  } else {
    assertion('docker_runner_native_linux', process.platform === 'linux')
    const image = JSON.parse(invoke('docker', ['image', 'inspect', flags.image]))[0]
    assertion('image_non_root_user_preserved', image.Config.User === '65532:65532')
    assertion('image_exact_candidate_revision', image.Config.Labels?.['org.opencontainers.image.revision'] === flags.commit)
    assertion('image_matches_native_host_architecture', image.Architecture === ({ x64: 'amd64', arm64: 'arm64' }[process.arch]))
    report.candidate = { imageId: image.Id, architecture: image.Architecture,
      experimentalRecipeSha256: image.Config.Labels?.['solovey.qualification.overlay.sha256'] || null }
    if (report.candidate.experimentalRecipeSha256) report.evidenceClass = 'REPRODUCTION_PROVEN_PENDING_OWNER_INTEGRATION'
    // Only this marked, private fixture subtree changes ownership. The product
    // still executes as65532; no root container or extra capability is used.
    const expected = path.join(fs.realpathSync(temporary), 'data')
    assertion('fixture_chown_target_confined', fs.realpathSync(data) === expected)
    invoke(process.getuid() === 0 ? 'chown' : 'sudo', process.getuid() === 0
      ? ['-R', '65532:65532', '--', data] : ['-n', 'chown', '-R', '65532:65532', '--', data])
    const name = `solovey-w5-${crypto.randomBytes(8).toString('hex')}`
    const args = ['run', '--detach', '--name', name, '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
      '--tmpfs', '/run/solovey-ui:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532',
      '--tmpfs', '/tmp:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532',
      '--mount', `type=bind,src=${data},dst=/data`, '-e', 'SUI_SERVER_PROTECTION_RUNTIME_ROOT=/run/solovey-ui/server-protection',
      '-p', `127.0.0.1::${webPort}/tcp`, ...Object.values(ports).flatMap((port) => ['-p', `127.0.0.1::${port}/udp`]), flags.image]
    if (flags['private-probe']) {
      assertion('docker_private_probe_is_regular_fixture_file', fs.statSync(path.resolve(flags['private-probe'])).isFile())
      args.splice(args.length - 1, 0, '--mount', `type=bind,src=${path.resolve(flags['private-probe'])},dst=/opt/w5/private-api-probe,readonly`)
      report.fixtureSource.privateProbeSha256 = hash(path.resolve(flags['private-probe']))
    }
    if (flags['udp-witness']) {
      assertion('docker_udp_witness_regular_fixture_file', fs.statSync(path.resolve(flags['udp-witness'])).isFile())
      args.splice(args.length - 1, 0, '--mount', `type=bind,src=${path.resolve(flags['udp-witness'])},dst=/opt/w5/udp-witness,readonly`)
    }
    if (flags['ld-preload']) {
      assertion('experimental_loader_is_existing_image_dependency', flags['ld-preload'] === '/lib/libgcompat.so.0')
      args.splice(args.length - 1, 0, '-e', `LD_PRELOAD=${flags['ld-preload']}`)
      report.experimentalRuntimeOverlay = { LD_PRELOAD: flags['ld-preload'], sourceUnchanged: true,
        qualification: 'REPRODUCTION_PROVEN_ONLY_UNTIL_OWNER_INTEGRATION' }
    }
    container = invoke('docker', args).trim()
    const actual = JSON.parse(invoke('docker', ['inspect', container]))[0]
    assertion('container_readonly_no_caps_no_new_privileges', actual.HostConfig.ReadonlyRootfs && actual.HostConfig.CapDrop.includes('ALL') && actual.HostConfig.SecurityOpt.includes('no-new-privileges'))
    const mappings = actual.NetworkSettings.Ports
    assertion('all_published_ports_loopback_only', Object.values(mappings).flat().every((mapping) => mapping.HostIp === '127.0.0.1'))
    input.API = `http://127.0.0.1:${mappings[`${webPort}/tcp`][0].HostPort}/app/`
    input.Ports = Object.fromEntries(Object.entries(ports).map(([kind, port]) => [kind, Number(mappings[`${port}/udp`][0].HostPort)]))
    await waitReady(`${input.API}login`)
    if (flags['private-probe']) {
      const deadline = Date.now() + 45000
      let privateListener
      while (Date.now() < deadline) {
        const sockets = invoke('docker', ['exec', container, 'cat', '/proc/net/tcp']).trim().split(/\r?\n/).slice(1)
          .map((line) => line.trim().split(/\s+/)).filter((columns) => columns[3] === '0A')
          .map((columns) => ({ address: columns[1].split(':')[0], port: parseInt(columns[1].split(':')[1], 16) }))
          .filter((socket) => ![webPort, subPort].includes(socket.port))
        if (sockets.length === 1 && sockets[0].address === '0100007F') { privateListener = sockets[0]; break }
        await new Promise((resolve) => setTimeout(resolve, 100))
      }
      assertion('container_single_private_api_loopback_listener_unpublished', !!privateListener && !mappings[`${privateListener.port}/tcp`])
      const privateResult = JSON.parse(invoke('docker', ['exec', container, '/opt/w5/private-api-probe', String(privateListener.port)]))
      assertion('negative_private_api_probe_runs_as_product_uid', privateResult.uid === 65532 && privateResult.result === 'PASS')
      report.assertions.push(...privateResult.assertions)
    }
    const metadata = JSON.parse(invoke('docker', ['exec', container, 'cat', '/app/QUIC_INTEGRATION.json']))
    report.quicIntegration = metadata
    const ssmMetadata = spawnSync('docker', ['exec', container, 'cat', '/app/SSM_INTEGRATION.json'], {encoding: 'utf8', timeout: 10000})
    if (ssmMetadata.status === 0) report.ssmIntegration = JSON.parse(ssmMetadata.stdout)
    const nativeMetadata = spawnSync('docker', ['exec', container, 'cat', '/app/CRONET_INTEGRATION.json'], { encoding: 'utf8', timeout: 10000 })
    if (nativeMetadata.status === 0) report.cronetIntegration = JSON.parse(nativeMetadata.stdout)
    report.udpListeners = { ipv4: invoke('docker', ['exec', container, 'cat', '/proc/net/udp']),
      ipv6: invoke('docker', ['exec', container, 'cat', '/proc/net/udp6']),
      expectedInternalPorts: ports, published: input.Ports }
    const copied = path.join(temporary, 'candidate-binary')
    invoke('docker', ['cp', `${container}:/app/solovey-ui`, copied])
    report.candidate.binarySha256 = hash(copied)
    const health = invoke('docker', ['exec', container, '/app/solovey-ui', 'doctor', '--panel'])
    assertion('container_configured_health_probe', health.includes('panel: configured listener is reachable'))
  }
  input.Certificate = seed.certificate
  input.Clients = seed.clients
  if (flags['udp-witness']) input.UDPPort = await startUDPWitness()
  const result = helper('probe', input)
  report.assertions.push(...result.assertions)
  if (result.logSubscriptionWallMs !== undefined) report.logSubscriptionWallMs = result.logSubscriptionWallMs
  if (flags['browser-module']) {
    assertion('browser_frontend_dependency_root_available', !!flags.frontend && fs.existsSync(path.join(flags.frontend, 'package.json')))
    report.fixtureSource.browserModuleSha256 = hash(path.resolve(flags['browser-module']))
    const {exercisePackagedBrowser} = await import(pathToFileURL(path.resolve(flags['browser-module'])).href)
    report.browser = await exercisePackagedBrowser({frontend: flags.frontend, endpoint: input.API.replace(/api\/?$/, ''),
      username: input.AdminUser, password: input.AdminPassword})
    report.assertions.push(...report.browser.assertions)
  }
  if (process.platform === 'win32') {
    const program = path.join(process.env.SystemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe')
    const loaded = invoke(program, ['-NoProfile', '-NonInteractive', '-Command',
      `$ErrorActionPreference='Stop'; @((Get-Process -Id ${child.pid}).Modules | Where-Object ModuleName -eq 'libcronet.dll').Count`]).trim()
    assertion('packaged_naive_engine_loaded_native_cronet_dll', loaded === '1')
  }
  captureContainerLogs()
  assertion('candidate_logs_do_not_expose_fixture_secrets', !secretLeak)
  report.status = 'PASS'
}

try { await run() } catch (error) {
  report.status = 'FAILED_HARNESS_OR_PRODUCT_UNCLASSIFIED'
  report.failure = String(error.message).replace(/[^A-Za-z0-9_:.-]/g, '_').slice(0, 200)
} finally {
  captureContainerLogs()
  report.sanitizedDiagnostics = sanitizedDiagnostics()
  report.secretLeakDetected = secretLeak
  if (container) { try { invoke('docker', ['stop', '--time', '10', container]); invoke('docker', ['rm', container]); report.containerCleanup = 'REMOVED_OWNED_CONTAINER' } catch { report.containerCleanup = 'FAILED' } }
  if (witness && witness.exitCode === null) {
    witness.kill('SIGTERM')
    await Promise.race([new Promise(resolve => witness.once('exit', resolve)), new Promise(resolve => setTimeout(resolve, 1000))])
    if (witness.exitCode === null) witness.kill('SIGKILL')
  }
  if (child && child.exitCode === null) {
    child.kill('SIGTERM')
    await Promise.race([new Promise((resolve) => child.once('exit', resolve)), new Promise((resolve) => setTimeout(resolve, 5000))])
    if (child.exitCode === null) child.kill('SIGKILL')
    report.processTeardown = process.platform === 'win32' ? 'TERMINATEPROCESS_CLEANUP_NOT_GRACEFUL_PROOF' : 'SIGTERM'
  }
  fs.mkdirSync(path.dirname(path.resolve(flags.output)), { recursive: true })
  report.finishedAt = new Date().toISOString()
  report.fixtureRetainedForPrivateInvestigation = report.status !== 'PASS'
  if (report.status === 'PASS') {
    const resolved = fs.realpathSync(temporary)
    if (path.dirname(resolved) !== fs.realpathSync(os.tmpdir()) || !path.basename(resolved).startsWith('solovey-w5-package-')) throw new Error('FIXTURE_CLEANUP_TARGET_INVALID')
    if (container && process.getuid() !== 0) invoke('sudo', ['-n', 'chown', '-R', `${process.getuid()}:${process.getgid()}`, '--', data])
    fs.rmSync(resolved, { recursive: true })
    report.cleanup = 'REMOVED_ONLY_OWNED_DISPOSABLE_FIXTURE_AFTER_RESULT'
  } else report.cleanup = 'PRIVATE_FIXTURE_RETAINED'
  fs.writeFileSync(flags.output, JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify({ status: report.status, assertions: report.assertions.length, failure: report.failure, report: path.basename(flags.output) }))
}
if (report.status !== 'PASS') process.exitCode = 1
