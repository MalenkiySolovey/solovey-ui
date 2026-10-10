import { describe, expect, it } from 'vitest'
import { compareUsedTraffic, usedTraffic } from '@/shared/clients/traffic'
import { sortItems } from '@/components/nexus/data/dataTableColumns'

describe('client used traffic ordering shared by Classic and Nexus', () => {
  it('orders numeric traffic independently of quota, including unlimited clients', () => {
    const clients = [
      { id: 1, up: 10, down: 5, volume: 0 },
      { id: 2, up: 1, down: 1, volume: 1000 },
      { id: 3, up: 100, down: 2, volume: 200 },
      { id: 4, up: 0, down: 0, volume: 5000 },
    ]
    const original = clients.map(client => ({ ...client }))
    expect([...clients].sort(compareUsedTraffic).map(client => client.id)).toEqual([4, 2, 1, 3])
    const columns = [{ key: 'volume', sortValue: usedTraffic }]
    expect(sortItems(clients, { key: 'volume', direction: 'asc' }, columns).map(client => client.id)).toEqual([4, 2, 1, 3])
    expect(sortItems(clients, { key: 'volume', direction: 'desc' }, columns).map(client => client.id)).toEqual([3, 1, 2, 4])
    expect(clients).toEqual(original)
  })
})
