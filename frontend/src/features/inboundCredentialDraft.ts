import { secretMutation } from './secretCredentialDraft'

export function serializeInboundCredentialDraft<T extends { type: string; host_key?: unknown }>(inbound: T): T {
  const result = { ...inbound }
  if (inbound.type === 'ssh') {
    const mutation = secretMutation(inbound.host_key)
    if (mutation === undefined) delete result.host_key
    else result.host_key = mutation
  }
  return result
}

export function inboundCredentialDraftInvalid(inbound: { type: string; host_key?: unknown }): boolean {
  try { serializeInboundCredentialDraft(inbound); return false } catch { return true }
}
