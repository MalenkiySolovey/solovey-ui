import { describe, expect, it } from 'vitest'
import { serializeOutboundCredentialDraft } from './outboundCredentialDraft'

describe('Outbound SecretRef save boundary', () => {
  it('keeps the optional Realm and its token at the existing Outbound owner', () => {
    const reference = { owner: 'outbounds', id: 'record:7:realm.token', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }
    const token = { reference, present: true, rotationRevision: 'b'.repeat(64), correlation: { intentId: 'presence' } }
    const realm = { server_url: 'https://realm.example', realm_id: 'room', stun_servers: ['stun.example:3478'], token, port_mapping: { enabled: false, timeout: '2s' } }
    const submitted = serializeOutboundCredentialDraft({ type: 'hysteria2', realm })
    expect(submitted.realm).toEqual({ ...realm, token: { intent: 'unchanged', reference, present: true, rotationRevision: 'b'.repeat(64) } })
    expect(realm.token).toBe(token)
    expect(serializeOutboundCredentialDraft({ type: 'hysteria2', server: 'direct.example' })).not.toHaveProperty('realm')
  })
  it('converts normal read views without changing the nested profile or editor draft', () => {
    const reference = { owner: 'outbounds', id: 'record:7:profile.auth_token', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }
    const view = { reference, present: true, rotationRevision: 'a'.repeat(64), correlation: { intentId: 'credential-presence' } }
    const draft = { type: 'masque', profile: { id: 'device', recreate: true, auth_token: view, license_key: { intent: 'clear' } } }
    const submitted = serializeOutboundCredentialDraft(draft)
    expect(submitted.profile).toEqual({ id: 'device', recreate: true, auth_token: { intent: 'unchanged', reference, present: true, rotationRevision: 'a'.repeat(64) }, license_key: { intent: 'clear' } })
    expect(draft.profile.auth_token).toBe(view)
    expect(draft.profile.auth_token).toHaveProperty('correlation')
    for (const type of ['mieru', 'trusttunnel']) {
      expect(serializeOutboundCredentialDraft({ type, password: view }).password).toEqual({ intent: 'unchanged', reference, present: true, rotationRevision: 'a'.repeat(64) })
    }
  })
})
