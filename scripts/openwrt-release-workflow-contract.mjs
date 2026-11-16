import assert from 'node:assert/strict'

export function verifyLinuxReleaseBuilder(workflow) {
  const heading = /^  build-linux:[ \t]*\r?$/m.exec(workflow)
  assert.ok(heading, 'Linux release build job is missing')
  const following = workflow.slice(heading.index + heading[0].length)
  const nextJob = /^  [A-Za-z_][A-Za-z0-9_-]*:[ \t]*\r?$/m.exec(following)
  const job = following.slice(0, nextJob?.index ?? following.length)
  assert.match(job, /bash scripts\/build-linux-target\.sh --mode release --profile full/)
  assert.match(job, /bash scripts\/build-linux-target\.sh --mode release --profile core/)
  assert.doesNotMatch(job, /\bgo build\b/, 'Linux Product builds must use the target builder')
}
