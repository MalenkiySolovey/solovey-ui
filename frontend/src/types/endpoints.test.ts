import { describe, expect, it } from 'vitest'

import { EpTypes, createEndpoint } from './endpoints'

describe('createEndpoint', () => {
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
