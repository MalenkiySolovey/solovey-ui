import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import { resolveBuildPlan } from './core-provider-build.mjs'
import { readExtendedProvenance, verifyExtendedProvenance } from './extended-integration-provenance.mjs'

test('Extended source notice binds native baseline, dedicated copy, checksums and canonical Git series',() => {
  const lock=readExtendedProvenance()
  assert.equal(lock.runtimeSemanticVersion,'1.14.3')
  assert.equal(lock.sources[0].original.commit,'d9442d14b19ddd02605bf3ee893a171947038e19')
  assert.equal(lock.sources[1].original.commit,'75c3ac4fa12b4ded2dc01b392ed7d5eaf3ea6876')
  assert.equal(lock.sources[0].replacement.repository,'https://github.com/MalenkiySolovey/sing-box-extended')
  assert.equal(lock.sources[0].patchSeries.patchCount,58)
  assert.ok(lock.sources.every(source => !Object.hasOwn(source,'patch')))
})

test('Extended provenance rejects changed authority, copy, checksums, transport and Git series',() => {
  const original=readExtendedProvenance()
  const input={plan:resolveBuildPlan({provider:'shtorm-extended'}),mod:JSON.parse(execFileSync('go',['mod','edit','-json','go.extended.mod'],{encoding:'utf8',windowsHide:true})),sums:fs.readFileSync(new URL('../go.extended.sum',import.meta.url),'utf8')}
  for(const mutate of [
    value => {value.upstreamByteIdentical=true},
    value => {value.sources[0].original.commit='0'.repeat(40)},
    value => {value.sources[0].replacement.commit='0'.repeat(40)},
    value => {value.sources[1].replacement.sum='h1:changed'},
    value => {value.sources[0].patchSeries.identity='0'.repeat(64)},
    value => {value.sources[1].patchSeries.commits.reverse()},
    value => {value.sources[0].patchSeries.upstreamTree='unresolved'},
    value => {value.sources[0].replacement.repository='https://github.com/MalenkiySolovey/sing-box'},
    value => {value.sources[0].patch={file:'duplicate.patch'}},
    value => {value.transport.version='v0.61.0-sing-box-mod.7'},
  ]) {
    const lock=structuredClone(original)
    mutate(lock)
    assert.throws(() => verifyExtendedProvenance(lock,input),undefined,mutate.toString())
  }
})
