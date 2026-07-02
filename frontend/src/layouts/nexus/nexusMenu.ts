// Count keys map a menu entry to the reactive array on the Data() store whose
// length drives its sidebar badge. Only store-backed collections are listed;
// entries without a key simply render no badge.
import { computed } from 'vue'
import { navItems } from '@/componentSystem/registry'
import type { NavItem } from '@/componentSystem/types'

export type NexusCountKey =
  | 'inbounds'
  | 'clients'
  | 'outbounds'
  | 'endpoints'
  | 'services'
  | 'tlsConfigs'

export interface NexusMenuItem {
  title: string
  icon: string
  path: string
  singBoxSettings?: boolean
  countKey?: NexusCountKey
}

export interface NexusMenuGroup {
  // No labelKey -> the group renders without a subheader (e.g. Dashboard).
  labelKey?: string
  items: NexusMenuItem[]
}

const coreGroups: NexusMenuGroup[] = [
  {
    items: [
      { title: 'pages.home', icon: 'lucide:layout-grid', path: '/' },
    ],
  },
  {
    labelKey: 'nav.groups.proxy',
    items: [
      { title: 'pages.inbounds', icon: 'lucide:zap', path: '/inbounds', singBoxSettings: true, countKey: 'inbounds' },
      { title: 'pages.clients', icon: 'lucide:users', path: '/clients', countKey: 'clients' },
      { title: 'pages.outbounds', icon: 'lucide:arrow-up-right', path: '/outbounds', singBoxSettings: true, countKey: 'outbounds' },
      { title: 'pages.endpoints', icon: 'lucide:globe', path: '/endpoints', singBoxSettings: true, countKey: 'endpoints' },
      { title: 'pages.services', icon: 'lucide:server', path: '/services', singBoxSettings: true, countKey: 'services' },
    ],
  },
  {
    labelKey: 'nav.groups.network',
    items: [
      { title: 'pages.tls', icon: 'lucide:lock', path: '/tls', singBoxSettings: true, countKey: 'tlsConfigs' },
      { title: 'pages.rules', icon: 'lucide:list', path: '/rules', singBoxSettings: true },
      { title: 'pages.dns', icon: 'lucide:network', path: '/dns', singBoxSettings: true },
      { title: 'pages.singBoxConfig', icon: 'lucide:file-text', path: '/sing-box-config', singBoxSettings: true },
    ],
  },
  {
    labelKey: 'nav.groups.integrations',
    items: [],
  },
  {
    labelKey: 'nav.groups.system',
    items: [
      { title: 'pages.admins', icon: 'lucide:user-cog', path: '/admins' },
      { title: 'pages.settings', icon: 'lucide:settings', path: '/settings' },
      { title: 'pages.support', icon: 'lucide:heart-handshake', path: '/support' },
    ],
  },
]

const toNexusItem = (item: NavItem): NexusMenuItem => ({
  title: item.title,
  icon: item.nexusIcon ?? item.icon,
  path: item.path,
  singBoxSettings: item.singBoxSettings,
  countKey: item.countKey as NexusCountKey | undefined,
})

const groupContributions = (section: NonNullable<NavItem['section']>): NexusMenuItem[] =>
  navItems.value
    .filter(item => item.section === section)
    .sort((a, b) => (a.order ?? 1000) - (b.order ?? 1000))
    .map(toNexusItem)

export const nexusMenuGroups = computed<NexusMenuGroup[]>(() =>
  coreGroups
    .map(group => {
      if (group.labelKey === 'nav.groups.proxy') {
        return {
          ...group,
          items: [
            ...group.items.slice(0, 3),
            ...groupContributions('proxy'),
            ...group.items.slice(3),
          ],
        }
      }
      if (group.labelKey === 'nav.groups.integrations') {
        return {
          ...group,
          items: groupContributions('integrations'),
        }
      }
      if (group.labelKey === 'nav.groups.system') {
        return {
          ...group,
          items: [
            group.items[0],
            ...groupContributions('system'),
            ...group.items.slice(1),
          ],
        }
      }
      return group
    })
    .filter(group => group.items.length > 0),
)

// Flat projections preserved so existing consumers keep working; they are
// derived, never maintained by hand.
export const nexusMenu = computed<NexusMenuItem[]>(() => nexusMenuGroups.value.flatMap(group => group.items))

export const nexusSingBoxSettingsPaths = computed<string[]>(() =>
  nexusMenu.value
    .filter(item => item.singBoxSettings)
    .map(item => item.path),
)

export const visibleNexusMenuGroups = (): NexusMenuGroup[] => nexusMenuGroups.value
