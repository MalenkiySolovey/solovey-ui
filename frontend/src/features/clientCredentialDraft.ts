import RandomUtil from '@/plugins/randomUtil'
import { runtimeCapability, type RuntimeCapabilities } from '@/types/runtimeCapabilities'
import { secretMutation } from './secretCredentialDraft'

type Config = Record<string, Record<string, unknown>>

// Fixed Users owner field schemas; availability always comes from the backend.
export const managedClientSecretFields: Readonly<Record<string, string>> = {
  mieru: 'password', trusttunnel: 'password', ssh: 'password', mtproxy: 'secret',
}

export function managedCredentialAvailable(snapshot: RuntimeCapabilities | undefined, kind: string): boolean {
  return runtimeCapability(snapshot, 'inbounds', kind)?.available === true
}

export function initializeClientCredentialDrafts(config: Config, snapshot: RuntimeCapabilities | undefined, name: string) {
  for (const kind of Object.keys(managedClientSecretFields)) {
    if (!config[kind] && managedCredentialAvailable(snapshot, kind)) config[kind] = { name }
  }
}

export function serializeClientCredentialDrafts(config: Config): Config {
  const result = { ...config }
  for (const [kind, field] of Object.entries(managedClientSecretFields)) {
    if (!config[kind]) continue
    result[kind] = { ...config[kind] }
    const mutation = secretMutation(config[kind][field])
    if (mutation === undefined) delete result[kind][field]
    else result[kind][field] = mutation
  }
  return result
}

export function clientCredentialDraftInvalid(config: Config): boolean {
  try { serializeClientCredentialDrafts(config); return false } catch { return true }
}

export function shuffleManagedClientCredentials(config: Config, snapshot: RuntimeCapabilities | undefined, selected?: string) {
  for (const [kind, field] of Object.entries(managedClientSecretFields)) {
    if (config[kind] && (!selected || selected === kind) && managedCredentialAvailable(snapshot, kind)) {
      config[kind][field] = { intent: 'replace', value: kind === 'mtproxy' ? RandomUtil.randomMTProxySecret() : RandomUtil.randomSeq(32) }
    }
  }
}
