import { describe, expect, it } from 'vitest'
import { readCoreProviderIdentity } from './coreProvider'
import { selectSystemStatus } from '@/components/nexus/overview/selectors/systemStatusSelectors'

const source = {
  schema: 'solovey.core-provider-identity/v1', state: 'VERIFIED', id: 'shtorm-extended', name: 'Shtorm Extended',
  providerVersion: 'v1.14.3-extended-2.7.3', runtimeSemanticVersion: '1.14.3',
  module: 'github.com/shtorm-7/sing-box-extended', moduleSum: 'h1:Jo0rEFOGXZt+2QEkx0wSQPBDg750I4ad1oSKvOtCe5E=',
  sourceRepository: 'https://github.com/shtorm-7/sing-box-extended', sourceRevision: 'd9442d14b19ddd02605bf3ee893a171947038e19',
  upstreamRepository: 'https://github.com/SagerNet/sing-box', upstreamRevision: '7054cac5124465ffdb145870d7c01885566711b1',
  artifactState: 'UNKNOWN', artifactDigest: '', reasonCodes: ['ARTIFACT_DIGEST_NOT_OBSERVED'], revision: 'a'.repeat(64),
}

describe('CoreRuntime provider wire identity', () => {
  it('preserves provider and semantic versions without claiming artifact trust', () => {
    expect(readCoreProviderIdentity(source)).toEqual(source)
    expect(selectSystemStatus({ sbd: { provider: source, running: true } }).coreProvider).toEqual(source)
  })
  it('rejects malformed and contradictory evidence without inferring Official', () => {
    for (const input of [undefined, {}, { ...source, schema: 'other' }, { ...source, state: 'UNKNOWN' },
      { ...source, sourceRevision: '' }, { ...source, moduleSum: '' }, { ...source, artifactState: 'VERIFIED' },
      { ...source, artifactDigest: source.moduleSum }, { ...source, reasonCodes: ['<untrusted>'] }]) {
      expect(readCoreProviderIdentity(input)).toBeUndefined()
      expect(selectSystemStatus({ sbd: { provider: input } }).coreProvider).toBeUndefined()
    }
  })
  it('copies reason codes and omits unrecognized fields', () => {
    const value = readCoreProviderIdentity({ ...source, extra: 'opaque-should-not-be-carried' })!
    value.reasonCodes.push('LOCAL_TEST')
    expect(source.reasonCodes).toEqual(['ARTIFACT_DIGEST_NOT_OBSERVED'])
    expect(value).not.toHaveProperty('extra')
  })
})
