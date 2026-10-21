import { afterEach, describe, expect, it, vi } from 'vitest'
import { register as registerObservability } from '../../../components/observability-extra/frontend/index'
import {
  componentRoutes, contributionFor, navItems, registerBundledComponent, registerComponent,
  registeredComponents, setComponentUIReadPermissions, unregisterComponent,
} from './registry'
import type { ComponentContribution, RouteContribution } from './types'

afterEach(() => {
  registeredComponents.value.forEach(component => unregisterComponent(component.id))
  setComponentUIReadPermissions([])
  vi.restoreAllMocks()
})

const fixture = (path = '/fixture-page', name = 'component.fixture.page'): ComponentContribution => ({
  id: 'fixture', apiVersion: '1.0',
  routes: [{ path, name, component: async () => ({ default: {} }) }],
})

describe('controlled Component UI metadata', () => {
  it.each(['/inbounds', '/outbounds', '/rules', '/dns', '/dns/private', '/security/account', '/login', '/hosts/local/dns', '/:takeover(.*)*', '//example.org'])('rejects route takeover: %s', path => {
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    registerComponent(fixture(path))
    expect(contributionFor('fixture')).toBeUndefined()
  })

  it('rejects foreign route names, aliases, duplicate routes and cross-component collisions', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    registerComponent(fixture('/fixture-page', 'pages.inbounds'))
    expect(contributionFor('fixture')).toBeUndefined()
    const badAlias = fixture()
    badAlias.routes![0].alias = '/dns'
    registerComponent(badAlias)
    expect(contributionFor('fixture')).toBeUndefined()
    const duplicate = fixture()
    duplicate.routes!.push({ ...duplicate.routes![0], name: 'component.fixture.second' })
    registerComponent(duplicate)
    expect(contributionFor('fixture')).toBeUndefined()
    registerComponent(fixture())
    registerComponent({ ...fixture('/fixture-page', 'component.other.page'), id: 'other' })
    expect(contributionFor('fixture')).toBeDefined()
    expect(contributionFor('other')).toBeUndefined()
  })

  it('binds a bundled registration to its actual Component owner', () => {
    expect(() => registerBundledComponent('other', () => registerComponent(fixture()))).toThrow('owner mismatch')
    expect(contributionFor('fixture')).toBeUndefined()
  })

  it('keeps validated declarations immutable after registration', () => {
    const source = fixture()
    registerComponent(source)
    source.routes![0].path = '/dns'
    source.routes!.push({ ...source.routes![0], name: 'pages.dns' })
    expect(componentRoutes.value.map(route => route.path)).toEqual(['/fixture-page'])
    expect(() => contributionFor('fixture')!.routes!.push(source.routes![0])).toThrow()
  })

  it.each([
    { hostScope: 'node', surface: 'operations', permission: 'audit.read' },
    { hostScope: 'local', surface: 'arbitrary', permission: 'audit.read' },
    { hostScope: 'local', surface: 'operations', permission: 'invented.permission' },
  ])('rejects unknown access metadata', access => {
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const input = fixture()
    input.routes![0].access = access as RouteContribution['access']
    registerComponent(input)
    expect(contributionFor('fixture')).toBeUndefined()
  })

  it('projects the real Observability routes and nav from current read permissions', () => {
    registerBundledComponent('observability-extra', registerObservability)
    expect(componentRoutes.value).toHaveLength(0)
    expect(navItems.value).toHaveLength(0)
    setComponentUIReadPermissions(['diagnostics.read', 'unknown.permission'])
    expect(componentRoutes.value.map(route => route.name)).toEqual(['component.observability-extra.diagnostics'])
    expect(navItems.value.map(nav => nav.path)).toEqual(['/diagnostics'])
    setComponentUIReadPermissions(['audit.read', 'diagnostics.read'])
    expect(componentRoutes.value).toHaveLength(2)
    expect(componentRoutes.value.every(route => route.meta?.hostScope === 'local')).toBe(true)
    expect(navItems.value.map(nav => nav.path)).toEqual(['/audit', '/diagnostics'])
    unregisterComponent('observability-extra')
    expect(componentRoutes.value).toHaveLength(0)
    expect(navItems.value).toHaveLength(0)
  })
})
