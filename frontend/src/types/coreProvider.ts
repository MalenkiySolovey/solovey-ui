export interface CoreProviderIdentity {
  schema: 'solovey.core-provider-identity/v1'
  state: 'VERIFIED' | 'UNKNOWN'
  id: string
  name: string
  providerVersion: string
  runtimeSemanticVersion: string
  module: string
  moduleSum: string
  sourceRepository: string
  sourceRevision: string
  upstreamRepository: string
  upstreamRevision: string
  artifactDigest: string
  artifactState: 'VERIFIED' | 'UNKNOWN'
  reasonCodes: string[]
  revision: string
}

// Wire validation only. Names and support decisions remain backend-owned.
export function readCoreProviderIdentity(value: unknown): CoreProviderIdentity | undefined {
  if (!value || typeof value !== 'object') return
  const data = value as Record<string, unknown>
  const fields = ['id', 'name', 'providerVersion', 'runtimeSemanticVersion', 'module', 'moduleSum',
    'sourceRepository', 'sourceRevision', 'upstreamRepository', 'upstreamRevision', 'artifactDigest', 'revision'] as const
  if (data.schema !== 'solovey.core-provider-identity/v1' || !['VERIFIED', 'UNKNOWN'].includes(String(data.state)) ||
      !['VERIFIED', 'UNKNOWN'].includes(String(data.artifactState)) ||
      fields.some(key => typeof data[key] !== 'string' || (data[key] as string).length > 256) ||
      !Array.isArray(data.reasonCodes) || data.reasonCodes.length > 16 ||
      data.reasonCodes.some(code => typeof code !== 'string' || !/^[A-Z0-9_]{1,96}$/.test(code)) ||
      !/^[a-f0-9]{64}$/.test(data.revision as string)) return
  if (data.state === 'VERIFIED') {
    if (fields.filter(key => !['artifactDigest', 'revision'].includes(key)).some(key => !data[key]) ||
        !/^[a-f0-9]{40}$/.test(data.sourceRevision as string) || !/^[a-f0-9]{40}$/.test(data.upstreamRevision as string) ||
        !/^h1:[A-Za-z0-9+/]{43}=$/.test(data.moduleSum as string)) return
  } else if (fields.filter(key => !['artifactDigest', 'revision'].includes(key)).some(key => data[key] !== '')) return
  if (data.artifactState === 'UNKNOWN' ? data.artifactDigest !== '' : !/^[a-f0-9]{64}$/.test(data.artifactDigest as string)) return
  return {
    schema: data.schema, state: data.state as CoreProviderIdentity['state'],
    id: data.id as string, name: data.name as string, providerVersion: data.providerVersion as string,
    runtimeSemanticVersion: data.runtimeSemanticVersion as string, module: data.module as string,
    moduleSum: data.moduleSum as string, sourceRepository: data.sourceRepository as string,
    sourceRevision: data.sourceRevision as string, upstreamRepository: data.upstreamRepository as string,
    upstreamRevision: data.upstreamRevision as string, artifactDigest: data.artifactDigest as string,
    artifactState: data.artifactState as CoreProviderIdentity['artifactState'], reasonCodes: [...data.reasonCodes] as string[],
    revision: data.revision as string,
  }
}
