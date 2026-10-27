import { secretMutation } from './secretCredentialDraft'

type CredentialInbound = { type: string; host_key?: unknown; master_key?: unknown }

export function serializeInboundCredentialDraft<T extends CredentialInbound>(inbound: T): T {
  const result = { ...inbound }
  if (inbound.type === 'ssh') {
    const mutation = secretMutation(inbound.host_key)
    if (mutation === undefined) delete result.host_key
    else result.host_key = mutation
  }
  if (inbound.type === 'sudoku') {
    const mutation = secretMutation(inbound.master_key)
    if (mutation === undefined) delete result.master_key
    else result.master_key = mutation
  }
  return result
}

export function inboundCredentialDraftInvalid(inbound: CredentialInbound): boolean {
  try { serializeInboundCredentialDraft(inbound); return false } catch { return true }
}
