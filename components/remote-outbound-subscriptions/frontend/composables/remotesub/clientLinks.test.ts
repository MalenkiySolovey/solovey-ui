import { describe, expect, it } from 'vitest'
import { remoteClientSelectionValues, replaceRemoteClientLinks } from './clientLinks'
import type { Link } from '@/types/clients'

describe('remote subscription client links', () => {
  it('keeps links owned by other components while replacing remote selections', () => {
    const links: Link[] = [
      { type: 'futureComponentLink', uri: '', payload: { keep: true } },
      { type: 'remoteGroup', uri: '', groupId: 10 },
      { type: 'remoteSubscription', uri: '', subscriptionId: 20 },
    ]
    const names = new Map([
      ['group:30', 'Remote / Group'],
      ['subscription:40', 'Remote / All'],
    ])

    const replaced = replaceRemoteClientLinks(links, ['group:30', 'subscription:40'], names)

    expect(replaced).toEqual([
      { type: 'futureComponentLink', uri: '', payload: { keep: true } },
      { type: 'remoteGroup', groupId: 30, remark: 'Remote / Group', uri: '' },
      { type: 'remoteSubscription', subscriptionId: 40, remark: 'Remote / All', uri: '' },
    ])
    expect(remoteClientSelectionValues(replaced)).toEqual(['group:30', 'subscription:40'])
  })
})
