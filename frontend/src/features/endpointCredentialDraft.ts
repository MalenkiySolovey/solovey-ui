import { secretMutation } from './secretCredentialDraft'

// Both UI modes submit owner intents through this single endpoint boundary.
// Unknown native options are retained; masked API views are never plaintext.
export function serializeEndpointCredentialDraft<T extends { type: string }>(endpoint: T): T {
  if (endpoint.type !== 'openconnect') return endpoint
  const result = JSON.parse(JSON.stringify(endpoint)) as Record<string, any>
  for (const path of ['password', 'cookie', 'token.secret', 'token.pin', 'token.password', 'tls.client_key', 'tls.client_key_password', 'tls.mca_key', 'tls.mca_key_password', 'form_entries']) {
    const parts = path.split('.')
    let owner = result
    for (const part of parts.slice(0, -1)) owner = owner?.[part]
    if (!owner) continue
    const key = parts[parts.length - 1]
    const mutation = secretMutation(owner[key])
    if (mutation === undefined) delete owner[key]
    else owner[key] = mutation
  }
  return result as T
}
