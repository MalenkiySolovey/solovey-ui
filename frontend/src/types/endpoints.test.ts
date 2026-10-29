import { describe, expect, it } from 'vitest'

import { EpTypes, createEndpoint } from './endpoints'
import { serializeEndpointCredentialDraft } from '@/features/endpointCredentialDraft'

describe('createEndpoint', () => {
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
