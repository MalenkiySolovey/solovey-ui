import { secretMutation } from './secretCredentialDraft'

type CredentialInbound = { type: string; host_key?: unknown; master_key?: unknown; cookies?: unknown; password?: unknown; join_link?: unknown; realm?: { token?: unknown; server_url?: string; realm_id?: string; stun_servers?: string[] } }

export function serializeInboundCredentialDraft<T extends CredentialInbound>(inbound: T): T {
  const result = { ...inbound }
  if (inbound.type === 'hysteria2' && inbound.realm) {
    const realm = { ...inbound.realm }
    const mutation = secretMutation(realm.token)
    if (mutation === undefined) delete realm.token
    else realm.token = mutation
    result.realm = realm
  }
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
  if (inbound.type === 'call') {
    for (const field of ['cookies', 'password', 'join_link'] as const) {
      const mutation = secretMutation(inbound[field])
      if (mutation === undefined) delete result[field]
      else result[field] = mutation
    }
  }
  return result
}

export function inboundCredentialDraftInvalid(inbound: CredentialInbound): boolean {
  try {
    serializeInboundCredentialDraft(inbound)
    return inbound.type === 'hysteria2' && inbound.realm != null && (!inbound.realm.server_url?.trim() || !inbound.realm.realm_id?.trim() || !inbound.realm.stun_servers?.length)
  } catch { return true }
}
