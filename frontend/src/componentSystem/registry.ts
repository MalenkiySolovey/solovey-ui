import { computed, reactive, ref } from 'vue'
import { snapshotContribution, uiReadPermissions, validateUIContribution } from './uiContract'
import type { ComponentUIReadPermission } from './types'
import type { ComponentContribution, NavItem, RegisteredContribution, SettingsTab, SlotContribution, SlotName, RouteContribution } from './types'

const HOST_API_VERSION = '1.0'

const components = reactive(new Map<string, RegisteredContribution>())
const readPermissions = ref<ComponentUIReadPermission[]>([])
let registeringOwner: string | undefined

export const setComponentUIReadPermissions = (permissions: unknown) => {
  readPermissions.value = Array.isArray(permissions)
    ? uiReadPermissions.filter(permission => permissions.includes(permission)) : []
}

export const registerBundledComponent = (id: string, register: () => void) => {
  const previous = registeringOwner
  registeringOwner = id
  try { register() } finally { registeringOwner = previous }
}

const routeVisible = (route: RouteContribution) => !route.access || readPermissions.value.includes(route.access.permission)

const ordered = <T extends { order?: number }>(items: T[]): T[] =>
  [...items].sort((a, b) => (a.order ?? 1000) - (b.order ?? 1000))

const compatible = (contribution: ComponentContribution): boolean => {
  if (!contribution.id || !/^[a-z0-9-]+$/.test(contribution.id)) {
    console.warn(`[componentSystem] invalid component id: ${contribution.id}`)
    return false
  }
  if (contribution.apiVersion !== HOST_API_VERSION) {
    console.warn(`[componentSystem] ${contribution.id} requires frontend API ${contribution.apiVersion}, host is ${HOST_API_VERSION}`)
    return false
  }
  return true
}

export const registerComponent = (contribution: ComponentContribution) => {
  if (registeringOwner && registeringOwner !== contribution.id) throw new Error('Component UI owner mismatch')
  if (!compatible(contribution) || !validateUIContribution(contribution, [...components.values()])) {
    console.warn(`[componentSystem] rejected UI contribution: ${contribution.id}`)
    return
  }
  components.set(contribution.id, Object.freeze({
    ...snapshotContribution(contribution),
    registeredAt: Date.now(),
  }))
}

export const unregisterComponent = (id: string) => {
  components.delete(id)
}

export const registeredComponents = computed<RegisteredContribution[]>(() =>
  [...components.values()].sort((a, b) => a.id.localeCompare(b.id)),
)

export const componentRoutes = computed<RouteContribution[]>(() =>
  registeredComponents.value.flatMap(component =>
    (component.routes ?? []).filter(routeVisible).map(route => ({
      ...route,
      meta: {
        ...(route.meta ?? {}),
        componentId: component.id,
        hostScope: route.access?.hostScope,
        uiPermission: route.access?.permission,
      },
    })),
  ),
)

export const navItems = computed<NavItem[]>(() =>
  ordered(registeredComponents.value.flatMap(component => (component.nav ?? []).flatMap(item => {
    if (!('route' in item)) return [item]
    const route = component.routes?.find(route => route.name === item.route)
    if (!route || !routeVisible(route)) return []
    return [{ ...item, path: route.path }]
  }))),
)

export const settingsTabs = computed<SettingsTab[]>(() =>
  ordered(registeredComponents.value.flatMap(component => component.settingsTabs ?? [])),
)

export const slotEntries = (slot: SlotName): SlotContribution[] =>
  ordered(registeredComponents.value.flatMap(component =>
    (component.slots ?? []).filter(entry => entry.slot === slot),
  ))

export const contributionFor = (id: string): RegisteredContribution | undefined => components.get(id)
