import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { resolveBuildPlan, verifySourcePin } from './core-provider-build.mjs'

const catalog=JSON.parse(fs.readFileSync(new URL('../core/provider/pins.json',import.meta.url),'utf8'))

test('default build retains the accepted Official source and caller tags',() => {
  const plan=resolveBuildPlan({baseTags:'with_quic,with_utls'})
  assert.equal(plan.provider,'sing-box')
  assert.equal(plan.source.runtimeSemanticVersion,'1.14.2')
  assert.equal(plan.modFile,'go.mod')
  assert.equal(plan.tags,'with_quic,with_utls')
  assert.deepEqual(plan.goArgs,['-mod=readonly'])
})

test('explicit Extended build uses its immutable source and separate native lock',() => {
  const plan=resolveBuildPlan({provider:'shtorm-extended',baseTags:'with_quic,with_utls'})
  assert.equal(plan.source.runtimeSemanticVersion,'1.14.3')
  assert.equal(plan.source.upstreamRevision,'d9442d14b19ddd02605bf3ee893a171947038e19')
  assert.equal(plan.modFile,'go.extended.mod')
  assert.equal(plan.tags,'with_quic,with_utls,shtorm_extended,with_gvisor,with_openconnect,with_openvpn')
  assert.deepEqual(plan.goArgs,['-mod=readonly','-modfile=go.extended.mod'])
})

test('unsupported providers and contradictory or malformed tags fail before build',() => {
  for(const provider of ['unknown','../go.mod','', 'deposist'])assert.throws(() => resolveBuildPlan({provider}),/Unknown core provider/)
  assert.throws(() => resolveBuildPlan({baseTags:'with_quic,shtorm_extended'}),/contradicts/)
  for(const baseTags of ['with_quic;run','with_quic,','!shtorm_extended'])assert.throws(() => resolveBuildPlan({baseTags}),/Invalid base/)
})

test('modified engine requirements, local replacements and checksum drift fail closed',() => {
  const source=catalog.sources.find(item => item.sourceRevision===catalog.buildFlavors[1].sourceRevision)
  const mod={Require:[{Path:'github.com/sagernet/sing-box',Version:'v1.14.3'}],Replace:[{Old:{Path:'github.com/sagernet/sing-box'},New:{Path:source.module,Version:source.providerVersion}}]}
  const sums=`${source.module} ${source.providerVersion} ${source.moduleSum}\n`
  verifySourcePin(source,mod,sums)
  for(const mutate of [
    value => { value.Require[0].Version='v1.14.2' },
    value => { value.Replace[0].New={Path:'../local-engine'} },
    value => { value.Replace.push(value.Replace[0]) },
  ]) {
    const changed=structuredClone(mod)
    mutate(changed)
    assert.throws(() => verifySourcePin(source,changed,sums),/differs/)
  }
  assert.throws(() => verifySourcePin(source,mod,sums.replace(source.moduleSum,'h1:unverified')),/checksum differs/)
})
