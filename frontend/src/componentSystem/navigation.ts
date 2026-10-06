import { computed } from 'vue'
import { coreNavigation, type NavigationCountKey, type NavigationSection } from '@/navigation/core'
import { navItems } from './registry'
import type { NavItem } from './types'

export interface ClassicMenuItem { title: string; icon: string; path: string; order: number }
export interface NexusMenuItem { title: string; icon: string; path: string; singBoxSettings?: boolean; countKey?: NavigationCountKey }
export interface NexusMenuGroup { labelKey?: string; items: NexusMenuItem[] }

const toClassicItem = (item: NavItem): ClassicMenuItem => ({ title: item.title, icon: item.icon, path: item.path, order: item.order ?? 1000 })
const toNexusItem = (item: NavItem): NexusMenuItem => ({ title: item.title, icon: item.nexusIcon ?? item.icon, path: item.path, singBoxSettings: item.singBoxSettings, countKey: item.countKey as NavigationCountKey | undefined })
export const classicMenuItems = computed<ClassicMenuItem[]>(() =>
  [...coreNavigation, ...navItems.value].map(toClassicItem).sort((a, b) => a.order - b.order),
)

const sections: NavigationSection[] = ['dashboard', 'proxy', 'network', 'integrations', 'system']
export const nexusMenuGroups = computed<NexusMenuGroup[]>(() => sections.map(section => {
  const core = coreNavigation.filter(item => item.section === section)
  const contributed = navItems.value.filter(item => item.section === section)
    .sort((a, b) => (a.order ?? 1000) - (b.order ?? 1000)).map(toNexusItem)
  const items = core.flatMap(item => item.nexusContributionsAfter ? [toNexusItem(item), ...contributed] : [toNexusItem(item)])
  if (!core.some(item => item.nexusContributionsAfter)) items.push(...contributed)
  return { labelKey: section === 'dashboard' ? undefined : 'nav.groups.' + section, items }
}).filter(group => group.items.length > 0))
