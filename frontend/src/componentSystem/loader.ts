import type { Router, RouteRecordRaw } from 'vue-router'
import Data from '@/store/modules/data'
import { componentEntries } from 'virtual:solovey-component-entries'
import { componentRoutes, registeredComponents, registerComponent, unregisterComponent } from './registry'
import { loadComponentLocaleMessages } from './locales'

type ComponentEntryModule = { register: () => void; unregister?: () => void }
type ComponentEntryLoader = () => Promise<ComponentEntryModule>

const bundledEntries = componentEntries as Record<string, ComponentEntryLoader>

const entryByID = new Map<string, ComponentEntryLoader>(
  Object.entries(bundledEntries)
)

const loadedEntries = new Map<string, ComponentEntryModule>()
const routeRemovers = new Map<string, () => void>()
let syncInFlight: Promise<void> | undefined
let syncRequested = false

export const componentSystemBundledIDs = () => [...entryByID.keys()].sort()

export const syncEnabledComponents = (router: Router): Promise<void> => {
  syncRequested = true
  if (syncInFlight) return syncInFlight

  const request = (async () => {
    while (syncRequested) {
      syncRequested = false
      await syncEnabledComponentsOnce(router)
    }
  })()

  const pending = request.finally(() => {
    if (syncInFlight === pending) syncInFlight = undefined
    if (syncRequested) void syncEnabledComponents(router)
  })
  syncInFlight = pending
  return syncInFlight
}

const syncEnabledComponentsOnce = async (router: Router) => {
  const data = Data()
  if (!data.componentsLoaded) return

  const enabled = new Set(
    data.components
      .filter(component => component.installed && component.active)
      .map(component => component.id),
  )

  for (const component of registeredComponents.value) {
    if (!enabled.has(component.id)) {
      removeComponentRoutes(component.id)
      unloadComponent(component.id)
    }
  }

  for (const id of enabled) {
    if (!loadedEntries.has(id)) {
      const loader = entryByID.get(id)
      if (!loader) continue
      try {
        const module = await loader()
        module.register()
        loadedEntries.set(id, module)
      } catch (error) {
        console.warn(`[componentSystem] failed to load ${id}`, error)
        continue
      }
    }
    await loadComponentLocaleMessages(id)
  }

  syncComponentRoutes(router)
}

const unloadComponent = (id: string) => {
  const module = loadedEntries.get(id)
  if (module?.unregister) module.unregister()
  else unregisterComponent(id)
  loadedEntries.delete(id)
}

const syncComponentRoutes = (router: Router) => {
  const desired = new Set<string>()
  for (const route of componentRoutes.value) {
    const key = routeKey(route)
    desired.add(key)
    if (routeRemovers.has(key)) continue
    const record = {
      path: route.path,
      name: route.name,
      meta: route.meta,
      component: route.component as RouteRecordRaw['component'],
    } as RouteRecordRaw
    if (route.alias !== undefined) {
      record.alias = route.alias
    }
    routeRemovers.set(key, router.addRoute('main', record))
  }

  for (const [key, remove] of routeRemovers) {
    if (!desired.has(key)) {
      remove()
      routeRemovers.delete(key)
    }
  }
}

const removeComponentRoutes = (componentId: string) => {
  const prefix = `${componentId}:`
  for (const [key, remove] of routeRemovers) {
    if (key.startsWith(prefix)) {
      remove()
      routeRemovers.delete(key)
    }
  }
}

const routeKey = (route: { name: string; meta?: RouteRecordRaw['meta'] }) => {
  const meta = route.meta as { componentId?: unknown } | undefined
  return `${String(meta?.componentId ?? '')}:${route.name}`
}

export { registerComponent }
