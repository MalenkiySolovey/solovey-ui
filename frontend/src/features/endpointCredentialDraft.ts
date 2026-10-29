import { secretMutation } from './secretCredentialDraft'

// Both UI modes submit owner intents through this single endpoint boundary.
// Unknown native options are retained; masked API views are never plaintext.
export function serializeEndpointCredentialDraft<T extends { type: string }>(endpoint: T): T {
  const paths = endpoint.type === 'openconnect'
    ? ['password', 'cookie', 'token.secret', 'token.pin', 'token.password', 'tls.client_key', 'tls.client_key_password', 'tls.mca_key', 'tls.mca_key_password', 'form_entries']
    : endpoint.type === 'openvpn-client' ? ['password', 'static_key', 'tls.client_key', 'tls.control_wrap.key'] : []
  if (paths.length === 0) return endpoint
  const result = JSON.parse(JSON.stringify(endpoint)) as Record<string, any>
  for (const path of paths) {
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

// Called only after explicit mode-conversion confirmation. Clear owner fields
// through intents so an omitted key cannot silently retain the old credential.
export function convertOpenVPNModeDraft(endpoint: Record<string, any>, mode: 'tls' | 'static_key'): Record<string, any> {
  const result = JSON.parse(JSON.stringify(endpoint))
  result.mode = mode
  if (mode === 'static_key') {
    for (const key of ['username', 'auth_retry', 'static_challenge', 'static_challenge_echo', 'route_no_pull', 'pull_filters', 'renegotiate_interval', 'renegotiate_disabled', 'renegotiate_bytes', 'renegotiate_packets', 'tls_timeout', 'handshake_window', 'data_ciphers', 'data_ciphers_fallback']) delete result[key]
    result.password = { intent: 'clear' }
    result.tls = { client_key: { intent: 'clear' }, control_wrap: { key: { intent: 'clear' } } }
  } else {
    delete result.static_key_path
    delete result.key_direction
    delete result.cipher
    result.static_key = { intent: 'clear' }
    result.tls ??= {}
    result.data_ciphers ??= ['AES-256-GCM']
  }
  return result
}
