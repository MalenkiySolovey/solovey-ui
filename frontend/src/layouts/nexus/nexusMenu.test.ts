import { afterEach, describe, expect, it } from 'vitest'

import { registerComponent, unregisterComponent } from '@/componentSystem/registry'
import { nexusMenu, nexusMenuGroups, nexusSingBoxSettingsPaths, visibleNexusMenuGroups } from './nexusMenu'

const ids = ['test-remote', 'test-integrations', 'test-observability']

const registerOptionalMenu = () => {
  registerComponent({
    id: 'test-remote',
    apiVersion: '1.0',
    nav: [{ title: 'remoteOutbound.title', icon: 'mdi-cloud-download', nexusIcon: 'lucide:cloud-download', path: '/remote-subscriptions', section: 'proxy', singBoxSettings: true, order: 35 }],
  })
  registerComponent({
    id: 'test-integrations',
    apiVersion: '1.0',
    nav: [
      { title: 'telegram.title', icon: 'mdi-send', nexusIcon: 'lucide:send', path: '/telegram', section: 'integrations', order: 70 },
      { title: 'paidSub.title', icon: 'mdi-cash-multiple', nexusIcon: 'lucide:credit-card', path: '/paid-subscriptions', section: 'integrations', order: 80 },
    ],
  })
  registerComponent({
    id: 'test-observability',
    apiVersion: '1.0',
    nav: [
      { title: 'audit.title', icon: 'mdi-shield-search', nexusIcon: 'lucide:file-text', path: '/audit', section: 'system', order: 120 },
      { title: 'diagnostics.title', icon: 'mdi-clipboard-search', nexusIcon: 'lucide:gauge', path: '/diagnostics', section: 'system', order: 121 },
    ],
  })
}

afterEach(() => {
  for (const id of ids) unregisterComponent(id)
})

describe('nexus sing-box settings route parity', () => {
  it('keeps Nexus navigation wired to shared sing-box editor surfaces', () => {
    registerOptionalMenu()

    expect(nexusSingBoxSettingsPaths.value).toEqual(expect.arrayContaining([
      '/inbounds',
      '/outbounds',
      '/remote-subscriptions',
      '/endpoints',
      '/services',
      '/tls',
      '/rules',
      '/dns',
      '/sing-box-config',
    ]))
  })

  it('keeps each Nexus menu path unique', () => {
    registerOptionalMenu()
    const paths = nexusMenu.value.map(item => item.path)

    expect(new Set(paths).size).toBe(paths.length)
  })
})

describe('nexus grouped navigation integrity', () => {
  it('derives the flat menu from the groups without dropping any entry', () => {
    registerOptionalMenu()
    const grouped = nexusMenuGroups.value.flatMap(group => group.items)

    expect(grouped).toEqual(nexusMenu.value)
  })

  it('covers every required destination across the groups when components contribute their entries', () => {
    registerOptionalMenu()
    const paths = nexusMenuGroups.value.flatMap(group => group.items.map(item => item.path))

    expect(paths).toEqual(expect.arrayContaining([
      '/', '/inbounds', '/clients', '/outbounds', '/remote-subscriptions', '/endpoints', '/services',
      '/tls', '/rules', '/dns', '/sing-box-config', '/telegram', '/paid-subscriptions',
	  '/admins', '/security', '/ssh-management', '/audit', '/diagnostics', '/deployment', '/operations', '/settings', '/support',
    ]))
	expect(paths).toHaveLength(22)
  })

  it('labels every non-dashboard group with a nav.groups.* key', () => {
    registerOptionalMenu()
    const labelled = nexusMenuGroups.value.filter(group => group.labelKey)

    expect(labelled).toHaveLength(4)
    labelled.forEach(group => {
      expect(group.labelKey).toMatch(/^nav\.groups\./)
    })
  })

  it('leaves the first dashboard group without a subheader label', () => {
    expect(nexusMenuGroups.value[0].labelKey).toBeUndefined()
  })

  it('has no optional entries until components register contributions', () => {
    const paths = visibleNexusMenuGroups().flatMap(group => group.items.map(item => item.path))

    expect(paths).not.toContain('/remote-subscriptions')
    expect(paths).not.toContain('/telegram')
    expect(paths).not.toContain('/paid-subscriptions')
    expect(paths).not.toContain('/audit')
    expect(paths).not.toContain('/diagnostics')
    expect(paths).toContain('/outbounds')
  })
})


describe('shared core navigation owner', () => {
  it('feeds the exact same core descriptor set and labels to both layouts', async () => {
    const { coreNavigation } = await import('@/navigation/core')
    const { classicMenuItems } = await import('@/componentSystem/navigation')
    expect(classicMenuItems.value.map(item => item.path).sort()).toEqual(coreNavigation.map(item => item.path).sort())
    expect(nexusMenu.value.map(item => item.path).sort()).toEqual(coreNavigation.map(item => item.path).sort())
    for (const core of coreNavigation) {
      expect(classicMenuItems.value.find(item => item.path === core.path)).toMatchObject({ title: core.title, icon: core.icon })
      expect(nexusMenu.value.find(item => item.path === core.path)).toMatchObject({ title: core.title, icon: core.nexusIcon, singBoxSettings: core.singBoxSettings, countKey: core.countKey })
    }
  })
  it('projects reactive component registration/removal without changing core source', async () => {
    const { classicMenuItems } = await import('@/componentSystem/navigation')
    registerOptionalMenu()
    expect(classicMenuItems.value.map(item => item.path)).toContain('/telegram')
    expect(nexusMenu.value.map(item => item.path)).toContain('/telegram')
    for (const id of ids) unregisterComponent(id)
    expect(classicMenuItems.value.map(item => item.path)).not.toContain('/telegram')
    expect(nexusMenu.value.map(item => item.path)).not.toContain('/telegram')
  })
  it('preserves Classic ordering and Nexus contribution insertion points', async () => {
    const { classicMenuItems } = await import('@/componentSystem/navigation')
    registerOptionalMenu()
    const classic = classicMenuItems.value.map(item => item.path)
    expect(classic.indexOf('/outbounds')).toBeLessThan(classic.indexOf('/remote-subscriptions'))
    expect(classic.indexOf('/remote-subscriptions')).toBeLessThan(classic.indexOf('/endpoints'))
    const nexus = nexusMenu.value.map(item => item.path)
    expect(nexus.indexOf('/outbounds')).toBeLessThan(nexus.indexOf('/remote-subscriptions'))
    expect(nexus.indexOf('/remote-subscriptions')).toBeLessThan(nexus.indexOf('/endpoints'))
    expect(nexus.indexOf('/admins')).toBeLessThan(nexus.indexOf('/audit'))
    expect(nexus.indexOf('/diagnostics')).toBeLessThan(nexus.indexOf('/security'))
  })
})
