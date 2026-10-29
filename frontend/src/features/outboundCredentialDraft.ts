import { secretMutation } from './secretCredentialDraft'

type CredentialOutbound = {
  type: string
  profile?: { auth_token?: unknown; license_key?: unknown; private_key?: unknown }
  realm?: { token?: unknown }
}

// Normalize masked read views into explicit owner intents at the common
// Outbound save boundary. Keep profile metadata and inactive drafts intact.
export function serializeOutboundCredentialDraft<T extends CredentialOutbound>(outbound: T): T {
  const result: Record<string, unknown> = { ...outbound }
  const normalize = (target: Record<string, unknown>, fields: string[]) => {
    for (const field of fields) {
      const mutation = secretMutation(target[field])
      if (mutation === undefined) delete target[field]
      else target[field] = mutation
    }
  }
  if (outbound.type === 'call') normalize(result, ['cookies', 'join_link'])
  else if (['mieru', 'trusttunnel'].includes(outbound.type)) normalize(result, ['password'])
  else if (outbound.type === 'sudoku') normalize(result, ['key'])
  else if (outbound.type === 'masque') {
    const profile = { ...outbound.profile }
    normalize(profile, ['auth_token', 'license_key', 'private_key'])
    result.profile = profile
  }
  else if (outbound.type === 'hysteria2' && outbound.realm) {
    const realm = { ...outbound.realm }
    normalize(realm, ['token'])
    result.realm = realm
  }
  return result as T
}
