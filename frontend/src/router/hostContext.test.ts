import { createMemoryHistory, createRouter, type RouteRecordRaw } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { routes } from './routes'
import { hostChangeAllowed, hostIDFromPath, hostPath, routeHostID, safeHostReturnTo } from './hostContext'

const stubRoutes = (records: RouteRecordRaw[]): RouteRecordRaw[] => records.map(record => ({
  ...record,
  ...(record.redirect ? {} : { component: {} }),
  ...(record.children ? { children: stubRoutes(record.children) } : {}),
} as RouteRecordRaw))
const tab = () => createRouter({ history: createMemoryHistory('/panel/'), routes: stubRoutes(routes) })

describe('URL Host context', () => {
  it.each(['', 'inbounds', 'clients', 'outbounds', 'rules', 'dns', 'settings', 'operations'])(
    'defaults a legacy bookmark to Local and retains query/hash: %s', async page => {
      const router = tab()
      await router.push(`/${page}?tab=basics#details`)
      expect(router.currentRoute.value.fullPath).toBe(`${hostPath(`/${page}`)}?tab=basics#details`)
      expect(routeHostID(router.currentRoute.value)).toBe('local')
    },
  )

  it('retains the basics tab redirect and refresh/deep-link route identity', async () => {
    const first = tab()
    await first.push('/basics?source=bookmark#advanced')
    expect(first.currentRoute.value.fullPath).toBe('/hosts/local/settings?source=bookmark&tab=basics#advanced')
    const refreshed = tab()
    await refreshed.push(first.currentRoute.value.fullPath)
    expect(refreshed.currentRoute.value.name).toBe('pages.settings')
  })

  it('keeps two tabs independent without inventing a remote Host', async () => {
    const first = tab()
    const second = tab()
    await first.push('/hosts/local/dns')
    await second.push('/hosts/unavailable-example/outbounds')
    expect(routeHostID(first.currentRoute.value)).toBe('local')
    expect(routeHostID(second.currentRoute.value)).toBe('unavailable-example')
    await first.push({ name: 'pages.inbounds' })
    expect(first.currentRoute.value.fullPath).toBe('/hosts/local/inbounds')
    expect(second.currentRoute.value.fullPath).toBe('/hosts/unavailable-example/outbounds')
  })

  it('keeps absent feature bookmarks in explicit Host context', async () => {
    const router = tab()
    await router.push('/hosts/local/absent-component?view=history')
    expect(router.currentRoute.value.name).toBe('pages.unavailable')
    expect(router.currentRoute.value.fullPath).toBe('/hosts/local/absent-component?view=history')
  })

  it('blocks a dirty editor Host change until explicit discard', async () => {
    const router = tab()
    const discard = vi.fn(() => false)
    const draft = { name: 'unsaved credential' }
    router.beforeEach((to, from) => hostChangeAllowed(to, from, from.params.hostId === 'local', discard))
    await router.push('/hosts/local/clients')
    await router.push('/hosts/unavailable-example/clients')
    expect(router.currentRoute.value.fullPath).toBe('/hosts/local/clients')
    expect(draft.name).toBe('unsaved credential')
    expect(discard).toHaveBeenCalledOnce()
    discard.mockReturnValue(true)
    await router.push('/hosts/unavailable-example/clients')
    expect(routeHostID(router.currentRoute.value)).toBe('unavailable-example')
  })

  it('derives API binding from each URL, including configured panel bases', () => {
    expect(hostIDFromPath('/panel/hosts/local/clients', '/panel/')).toBe('local')
    expect(hostIDFromPath('/panel/hosts/other/dns', '/panel/')).toBe('other')
    expect(hostIDFromPath('/panel/hosts/%2F/dns', '/panel/')).toBe('/')
    expect(hostIDFromPath('/panel/hosts/%broken/dns', '/panel/')).toBe('invalid')
    expect(hostIDFromPath('/panel/login', '/panel/')).toBe('local')
  })

  it('retains only a Local deep-link return target through authentication', () => {
    expect(safeHostReturnTo('/hosts/local/dns?tab=servers#draft')).toBe('/hosts/local/dns?tab=servers#draft')
    for (const target of ['https://example.org', '//example.org', '/hosts/local-other/dns', '/hosts/other/dns', '/login', ['x']]) {
      expect(safeHostReturnTo(target)).toBeUndefined()
    }
  })
})
