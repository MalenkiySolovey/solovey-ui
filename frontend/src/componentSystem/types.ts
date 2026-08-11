import type { Component } from 'vue'
import type { RouteMeta } from 'vue-router'
import type { LocaleCode, LocaleMessages } from '@/locales'

export type SlotName =
  | 'sidebar:nav'
  | 'settings:tabs'
  | 'settings:subscription-advanced'
  | 'settings:maintenance'
  | 'backup-restore:backup-options'
  | 'backup-restore:actions'
  | 'dashboard:widgets'
  | 'client-editor:subscription-sources'
  | 'outbounds:row-actions'
  | 'inbound:editor'
  | 'inbound:status-detail'

// Inbound extensions receive the same draft object that the core modal is
// editing.  They may add component metadata, but must not save the inbound or
// call the core inbound API themselves.
export interface InboundSlotContext {
  inbound: Record<string, unknown>
  loading: boolean
  mode: 'add' | 'edit'
}

export type ComponentLoader = () => Promise<Component | { default: Component }>
export type LocaleLoader = () => Promise<{ default: LocaleMessages }>

export interface RouteContribution {
  path: string
  name: string
  component: ComponentLoader
  alias?: string | string[]
  meta?: RouteMeta
}

export interface NavItem {
  title: string
  icon: string
  nexusIcon?: string
  path: string
  order?: number
  section?: 'dashboard' | 'proxy' | 'network' | 'integrations' | 'system'
  singBoxSettings?: boolean
  countKey?: string
}

export interface SettingsTab {
  key: string
  title: string
  component: ComponentLoader
  order?: number
}

export interface SlotContribution {
  slot: SlotName
  component: ComponentLoader
  order?: number
}

export interface ComponentContribution {
  id: string
  apiVersion: string
  requiresCore?: string
  routes?: RouteContribution[]
  nav?: NavItem[]
  settingsTabs?: SettingsTab[]
  slots?: SlotContribution[]
  locales?: Partial<Record<LocaleCode, LocaleLoader>>
}

export interface RegisteredContribution extends ComponentContribution {
  registeredAt: number
}
