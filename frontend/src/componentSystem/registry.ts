import { computed, reactive } from 'vue'
import type { ComponentContribution, NavItem, RegisteredContribution, SettingsTab, SlotContribution, SlotName, RouteContribution } from './types'

const HOST_API_VERSION = '1.0'

const components = reactive(new Map<string, RegisteredContribution>())

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
  if (!compatible(contribution)) return
  components.set(contribution.id, {
    ...contribution,
    registeredAt: Date.now(),
  })
}

export const unregisterComponent = (id: string) => {
  components.delete(id)
}

export const registeredComponents = computed<RegisteredContribution[]>(() =>
  [...components.values()].sort((a, b) => a.id.localeCompare(b.id)),
)

export const componentRoutes = computed<RouteContribution[]>(() =>
  registeredComponents.value.flatMap(component =>
    (component.routes ?? []).map(route => ({
      ...route,
      meta: {
        ...(route.meta ?? {}),
        componentId: component.id,
      },
    })),
  ),
)

export const navItems = computed<NavItem[]>(() =>
  ordered(registeredComponents.value.flatMap(component => component.nav ?? [])),
)

export const settingsTabs = computed<SettingsTab[]>(() =>
  ordered(registeredComponents.value.flatMap(component => component.settingsTabs ?? [])),
)

export const slotEntries = (slot: SlotName): SlotContribution[] =>
  ordered(registeredComponents.value.flatMap(component =>
    (component.slots ?? []).filter(entry => entry.slot === slot),
  ))

export const contributionFor = (id: string): RegisteredContribution | undefined => components.get(id)
