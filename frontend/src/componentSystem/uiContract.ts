import { coreNavigation } from '@/navigation/core'
import type { ComponentContribution, ComponentUIReadPermission, RegisteredContribution } from './types'

export const uiReadPermissions: readonly ComponentUIReadPermission[] = ['audit.read', 'diagnostics.read']
const reservedPaths = new Set([...coreNavigation.map(item => item.path), '/login', '/security-transition', '/basics', '/hosts'])
const slots = new Set(['sidebar:nav', 'settings:tabs', 'settings:subscription-advanced', 'settings:maintenance',
  'backup-restore:backup-options', 'backup-restore:actions', 'dashboard:widgets', 'client-editor:subscription-sources',
  'outbounds:row-actions', 'inbound:editor', 'inbound:status-detail'])
const sections = new Set(['dashboard', 'proxy', 'network', 'integrations', 'system'])
const staticPath = (path: unknown): path is string => typeof path === 'string' && /^\/[a-z0-9-]+(?:\/[a-z0-9-]+)*$/.test(path) &&
  ![...reservedPaths].some(reserved => path === reserved || path.startsWith(`${reserved}/`))
const routePaths = (route: NonNullable<ComponentContribution['routes']>[number]) =>
  [route.path, ...(Array.isArray(route.alias) ? route.alias : route.alias ? [route.alias] : [])]

// Validation runs before a Component can reach Vue Router. This is first-party
// metadata admission, not a sandbox for arbitrary JS or remote bundles.
export const validateUIContribution = (input: ComponentContribution, existing: RegisteredContribution[]): boolean => {
  const paths = new Set<string>()
  const names = new Set<string>()
  for (const route of input.routes ?? []) {
    if (!route.name.startsWith(`component.${input.id}`) ||
      !(route.name === `component.${input.id}` || route.name.startsWith(`component.${input.id}.`)) || names.has(route.name)) return false
    names.add(route.name)
    if (typeof route.component !== 'function' || route.meta?.requiresAuth === false) return false
    if (route.access && (route.access.hostScope !== 'local' || route.access.surface !== 'operations' ||
      !uiReadPermissions.includes(route.access.permission))) return false
    for (const path of routePaths(route)) {
      if (!staticPath(path) || paths.has(path)) return false
      paths.add(path)
    }
  }
  const navPaths = new Set<string>()
  for (const nav of input.nav ?? []) {
    if (nav.section && !sections.has(nav.section)) return false
    const path = 'route' in nav ? input.routes?.find(route => route.name === nav.route)?.path : nav.path
    if (!staticPath(path) || navPaths.has(path)) return false
    navPaths.add(path)
  }
  for (const other of existing) {
    if (other.id === input.id) continue
    if (other.routes?.some(route => names.has(route.name) || routePaths(route).some(path => paths.has(path) || navPaths.has(path)))) return false
    if (other.nav?.some(nav => {
      const path = 'route' in nav ? other.routes?.find(route => route.name === nav.route)?.path : nav.path
      return path !== undefined && (paths.has(path) || navPaths.has(path))
    })) return false
  }
  if (input.slots?.some(slot => !slots.has(slot.slot) || typeof slot.component !== 'function')) return false
  const tabIDs = (input.settingsTabs ?? []).map(tab => tab.key)
  return new Set(tabIDs).size === tabIDs.length && !existing.some(other => other.id !== input.id &&
    other.settingsTabs?.some(tab => tabIDs.includes(tab.key)))
}

const frozenList = <T>(items: T[] | undefined): T[] | undefined => items ? Object.freeze(items) as unknown as T[] : undefined
export const snapshotContribution = (input: ComponentContribution): ComponentContribution => Object.freeze({
  ...input,
  routes: frozenList(input.routes?.map(route => Object.freeze({ ...route,
    alias: Array.isArray(route.alias) ? Object.freeze([...route.alias]) as unknown as string[] : route.alias,
    access: route.access ? Object.freeze({ ...route.access }) : undefined,
    meta: Object.freeze({ title: route.meta?.title, requiresAuth: true }),
  }))),
  nav: frozenList(input.nav?.map(item => Object.freeze({ ...item }))),
  settingsTabs: frozenList(input.settingsTabs?.map(tab => Object.freeze({ ...tab }))),
  slots: frozenList(input.slots?.map(slot => Object.freeze({ ...slot }))),
  locales: input.locales ? Object.freeze({ ...input.locales }) : undefined,
})
