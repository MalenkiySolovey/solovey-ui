import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

export function selectLatestAuditArtifacts(artifacts) {
  const byName = new Map()
  const ids = new Set()
  for (const artifact of artifacts) {
    assert.ok(Number.isSafeInteger(artifact.id) && artifact.id > 0, 'artifact ID required')
    assert.ok(!ids.has(artifact.id), 'duplicate artifact ID')
    ids.add(artifact.id)
    if (!artifact.name.startsWith('audit-go-') || artifact.name === 'audit-go-dashboard') continue
    assert.match(artifact.name, /^audit-go-[A-Za-z0-9_.-]+$/, 'safe artifact directory required')
    const created = Date.parse(artifact.created_at)
    assert.ok(Number.isFinite(created), 'artifact creation time required')
    const previous = byName.get(artifact.name)
    if (previous) assert.notEqual(created, Date.parse(previous.created_at), 'ambiguous artifact creation time')
    if (!previous || created > Date.parse(previous.created_at)) byName.set(artifact.name, artifact)
  }
  const selected = [...byName.values()].sort((a, b) => a.name.localeCompare(b.name))
  assert.ok(selected.length > 0, 'audit job artifacts required')
  for (const artifact of selected) {
    assert.equal(artifact.expired, false, 'latest audit artifact expired')
    assert.match(artifact.digest, /^sha256:[a-f0-9]{64}$/, 'artifact SHA256 authority required')
  }
  return selected
}

export function downloadGoAuditArtifacts({ repository, runId, destination, execute = execFileSync }) {
  assert.match(repository ?? '', /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/)
  assert.match(String(runId ?? ''), /^[1-9][0-9]*$/)
  const api = `repos/${repository}/actions/runs/${runId}/artifacts?per_page=100`
  const options = { maxBuffer: 64 << 20, windowsHide: true }
  const pages = JSON.parse(execute('gh', ['api', '--paginate', '--slurp', api], options).toString('utf8'))
  assert.ok(pages.length > 0, 'complete artifact pages required')
  const artifacts = pages.flatMap(page => page.artifacts)
  assert.ok(pages.every(page => page.total_count === artifacts.length), 'complete stable artifact inventory required')
  assert.ok(artifacts.every(artifact => String(artifact.workflow_run.id) === String(runId)), 'same-run artifacts required')
  const selected = selectLatestAuditArtifacts(artifacts)
  const output = path.resolve(destination)
  fs.mkdirSync(output, { recursive: true })
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'go-audit-download-'))
  try {
    for (const artifact of selected) {
      // Download the exact REST ID. The pinned download action's latest=true
      // selection sorts by numeric ID, which need not follow creation order.
      const bytes = execute('gh', ['api', '--allow-escape-sequences', `repos/${repository}/actions/artifacts/${artifact.id}/zip`], options)
      assert.equal(`sha256:${crypto.createHash('sha256').update(bytes).digest('hex')}`, artifact.digest, `${artifact.name}: archive digest mismatch`)
      const archive = path.join(scratch, `${artifact.id}.zip`)
      fs.writeFileSync(archive, bytes)
      const entries = execute('unzip', ['-Z1', archive], options).toString('utf8').trim().split('\n')
      assert.ok(entries.length > 0 && entries.every(entry => entry && !entry.includes('\\') && !/^(?:\/|[A-Za-z]:)/.test(entry) && !entry.split('/').includes('..')), 'safe archive paths required')
      const target = path.join(output, artifact.name)
      assert.equal(fs.existsSync(target), false, 'fresh per-artifact destination required')
      fs.mkdirSync(target)
      execute('unzip', ['-q', archive, '-d', target], options)
      console.log(`Verified audit artifact ${artifact.name}: ID ${artifact.id}, created ${artifact.created_at}`)
    }
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true })
  }
  const selectedIds = new Set(selected.map(artifact => artifact.id))
  const summary = artifact => ({ id: artifact.id, name: artifact.name, created_at: artifact.created_at, digest: artifact.digest })
  const receipt = { repository, runId: String(runId), selection: 'Newest creation time per audit job name; exact REST-ID download and SHA256 verification; prior dashboard excluded', selected: selected.map(summary), superseded: artifacts.filter(artifact => artifact.name.startsWith('audit-go-') && artifact.name !== 'audit-go-dashboard' && !selectedIds.has(artifact.id)).map(summary) }
  fs.writeFileSync(path.join(output, 'audit-artifact-selection.json'), `${JSON.stringify(receipt, null, 2)}\n`)
  return receipt
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  downloadGoAuditArtifacts({ repository: process.env.GITHUB_REPOSITORY, runId: process.env.GITHUB_RUN_ID, destination: process.argv[2] ?? 'tests/baseline' })
}
