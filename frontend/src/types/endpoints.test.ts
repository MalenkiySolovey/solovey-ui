import { describe, expect, it } from 'vitest'

import { EpTypes, createEndpoint, type Endpoint } from './endpoints'
import { convertOpenVPNModeDraft, serializeEndpointCredentialDraft } from '@/features/endpointCredentialDraft'

describe('createEndpoint', () => {
  it('restores native TLS and static-key options without injecting incompatible creation defaults', () => {
    for (const source of [
      { type: EpTypes.OpenVPNClient, id: 9, tag: 'legacy-vpn', server: '192.0.2.1', server_port: 1194, tls: {}, future_option: { retained: true } },
      { type: EpTypes.OpenVPNClient, id: 10, mode: 'static_key', servers: [{ server: '192.0.2.2', server_port: 1194 }], static_key: { present: true }, address: ['10.8.0.2/24'], peer_address: '10.8.0.1', cipher: 'AES-256-CBC' },
    ]) expect(createEndpoint<Endpoint>(EpTypes.OpenVPNClient, structuredClone(source))).toEqual(source)
    const first = createEndpoint(EpTypes.OpenVPNClient)
    first.servers[0].server = 'changed'
    expect(createEndpoint(EpTypes.OpenVPNClient).servers[0].server).toBe('')
  })
  it('converts VPN modes only through explicit credential clears and preserves unrelated native options', () => {
    const source = { type: EpTypes.OpenVPNClient, mode: 'tls', username: 'fixture', password: { present: true }, tls: { client_key: { present: true }, control_wrap: { type: 'tls_auth', key: { present: true } } }, data_ciphers: ['AES-256-GCM'], renegotiate_bytes: 100, pull_filters: [{ action: 'ignore', text: 'route' }], routes: ['192.0.2.0/24'], future_option: { retained: true } }
    const converted = convertOpenVPNModeDraft(source, 'static_key')
    expect(converted).toMatchObject({ mode: 'static_key', password: { intent: 'clear' }, tls: { client_key: { intent: 'clear' }, control_wrap: { key: { intent: 'clear' } } }, routes: source.routes, future_option: source.future_option })
    for (const key of ['username', 'data_ciphers', 'renegotiate_bytes', 'pull_filters']) expect(converted).not.toHaveProperty(key)
    const returned = convertOpenVPNModeDraft({ ...converted, cipher: 'AES-256-CBC', static_key_path: '/private/key', key_direction: 'client' }, 'tls')
    expect(returned).toMatchObject({ mode: 'tls', static_key: { intent: 'clear' }, data_ciphers: ['AES-256-GCM'] })
    for (const key of ['cipher', 'static_key_path', 'key_direction']) expect(returned).not.toHaveProperty(key)
    expect(source.tls.control_wrap.type).toBe('tls_auth')
    const view = { reference: { owner: 'endpoints', id: 'record:9:tls.control_wrap.key', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }, present: true, rotationRevision: 'a'.repeat(64) }
    expect(serializeEndpointCredentialDraft({ type: EpTypes.OpenVPNClient, tls: { control_wrap: { key: view } } }).tls.control_wrap.key).toEqual({ ...view, intent: 'unchanged' })
  })
  it('submits masked endpoint fields as unchanged owner intents without mutating the read view', () => {
    const view = { reference: { owner: 'endpoints', id: 'record:7:password', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }, present: true, rotationRevision: 'a'.repeat(64) }
    const source = createEndpoint('openconnect', { password: view, form_entries: { ...view, reference: { ...view.reference, id: 'record:7:form_entries' } }, token: { mode: 'totp', secret: { intent: 'replace', value: 'private-new-secret' } }, future_option: { retained: true } })
    const submitted = serializeEndpointCredentialDraft(source)
    expect(submitted.password).toEqual({ ...view, intent: 'unchanged' })
    expect(submitted.form_entries.intent).toBe('unchanged')
    expect(submitted.token.secret).toEqual(source.token.secret)
    expect(submitted.future_option).toEqual({ retained: true })
    expect(source.password).toEqual(view)
  })
  it('preserves OpenConnect native options and masked credentials without sharing defaults', () => {
    const source = { tag: 'vpn', password: { intent: 'unchanged', reference: { owner: 'endpoints', id: 'record:7:password' }, present: true, rotationRevision: 'ciphertext-revision' }, token: { mode: 'hotp', counter: 9 }, mobile: { device_type: 'fixture' } }
    const first = createEndpoint(EpTypes.OpenConnect, structuredClone(source))
    expect(first).toMatchObject(source)
    expect(first.flavor).toBe('anyconnect')
    first.token.counter = 10
    expect(source.token.counter).toBe(9)
    expect(createEndpoint(EpTypes.OpenConnect).token).toBeUndefined()
  })
  it('initializes fields required by a new WireGuard editor', () => {
    const endpoint = createEndpoint(EpTypes.Wireguard)

    expect(endpoint.peers).toEqual([])
    expect(endpoint.ext).toEqual({ keys: [] })
  })

  it('normalizes legacy WireGuard data and does not share mutable defaults', () => {
    const first = createEndpoint(EpTypes.Wireguard, { ext: null, peers: undefined } as any)
    const second = createEndpoint(EpTypes.Wireguard)

    first.peers.push({ address: '', port: 0, public_key: 'peer' })
    first.ext.keys.push({ public_key: 'peer' })

    expect(second.peers).toEqual([])
    expect(second.ext.keys).toEqual([])
  })
})
