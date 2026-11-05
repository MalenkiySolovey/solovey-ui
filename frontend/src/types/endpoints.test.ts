import { describe, expect, it } from 'vitest'

import { EpTypes, createEndpoint, type Endpoint } from './endpoints'
import { convertOpenVPNModeDraft, serializeEndpointCredentialDraft, setWireGuardAmneziaEnabled, wireGuardAmneziaEnabled } from '@/features/endpointCredentialDraft'
import Wireguard from '@/components/protocols/Wireguard.vue'

describe('createEndpoint', () => {
  it('preserves imported client policy and edits it without changing server peer routes', () => {
    const source = { type: EpTypes.Wireguard, peers: [{ allowed_ips: ['10.0.0.2/32'] }], ext: { dns: ['10.0.0.1', '2001:db8::53'], publicEndpoint: '[2001:db8::1]:63443', clientAllowedIPs: ['192.0.2.0/24'], clientKeepalive: 0, futurePolicy: { preserved: true } } }
    const draft = createEndpoint(EpTypes.Wireguard, source)
    const form = Wireguard as any
    const state = { $props: { data: draft } }
    expect(form.computed.dns.get.call(state)).toBe('10.0.0.1, 2001:db8::53')
    expect(form.computed.clientKeepalive.get.call(state)).toBe(0)
    form.computed.clientAllowedIPs.set.call(state, '198.51.100.0/24\n2001:db8:1::/64')
    form.computed.dns.set.call(state, '')
    form.computed.clientKeepalive.set.call(state, '')
    expect(draft.ext).toEqual({ ...source.ext, keys: [], dns: [], clientAllowedIPs: ['198.51.100.0/24', '2001:db8:1::/64'], clientKeepalive: undefined })
    expect(draft.peers).toEqual(source.peers)
    expect(source.ext.dns).toEqual(['10.0.0.1', '2001:db8::53'])
  })
  it('preserves native Amnezia ranges and owns the explicit disable credential intent', () => {
    const view = { reference: { owner: 'endpoints', id: 'record:9:amnezia.header_protection_key', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }, present: true, rotationRevision: 'a'.repeat(64) }
    const source = { type: EpTypes.Wireguard, amnezia: { h1: 123, h2: '200-300', header_protection_key: view, future_option: { retained: true } }, future_option: { retained: true } }
    const draft = createEndpoint(EpTypes.Wireguard, source)
    const submitted = serializeEndpointCredentialDraft(draft)
    expect(submitted.amnezia).toEqual({ ...source.amnezia, header_protection_key: { ...view, intent: 'unchanged' } })
    draft.amnezia.h1 = 124
    expect(source.amnezia.h1).toBe(123)
    setWireGuardAmneziaEnabled(draft, false)
    expect(wireGuardAmneziaEnabled(draft)).toBe(false)
    expect(serializeEndpointCredentialDraft(draft)).toMatchObject({ amnezia: { header_protection_key: { intent: 'clear' } }, future_option: source.future_option })
    setWireGuardAmneziaEnabled(draft, true)
    expect(draft.amnezia).toEqual({})
    expect(wireGuardAmneziaEnabled(draft)).toBe(true)
    expect(createEndpoint(EpTypes.Wireguard).amnezia).toBeUndefined()
  })
  it('restores native TLS and static-key options without injecting incompatible creation defaults', () => {
    for (const source of [
      { type: EpTypes.OpenVPNClient, id: 9, tag: 'legacy-vpn', server: '192.0.2.1', server_port: 1194, tls: {}, future_option: { retained: true } },
      { type: EpTypes.OpenVPNClient, id: 10, mode: 'static_key', servers: [{ server: '192.0.2.2', server_port: 1194 }], static_key: { present: true }, address: ['10.8.0.2/24'], peer_address: '10.8.0.1', cipher: 'AES-256-CBC' },
      { type: EpTypes.OpenVPNServer, id: 11, mode: 'static_key', static_key: { present: true }, address: ['10.8.0.1/24'], peer_address: '10.8.0.2', cipher: 'AES-256-CBC' },
    ]) expect(createEndpoint<Endpoint>(source.type, structuredClone(source))).toEqual(source)
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
    const server = convertOpenVPNModeDraft({ type: EpTypes.OpenVPNServer, users: { present: true }, tls: { key: { present: true } }, push: { dns: ['192.0.2.53'] }, duplicate_cn: true }, 'static_key')
    expect(server).toMatchObject({ users: { intent: 'clear' }, tls: { key: { intent: 'clear' } } })
    expect(server).not.toHaveProperty('password')
    for (const key of ['push', 'duplicate_cn']) expect(server).not.toHaveProperty(key)
    const tlsServer = convertOpenVPNModeDraft({ ...server, remote: '192.0.2.1', remote_port: 1194, peer_address: '10.8.0.2', peer_address_ipv6: 'fd00::2' }, 'tls')
    for (const key of ['remote', 'remote_port', 'peer_address', 'peer_address_ipv6']) expect(tlsServer).not.toHaveProperty(key)
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
