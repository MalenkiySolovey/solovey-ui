import { computed } from 'vue'
import { navItems } from './registry'
import type { NavItem } from './types'

export interface ClassicMenuItem {
  title: string
  icon: string
  path: string
  order: number
}

const coreClassicMenuItems: ClassicMenuItem[] = [
  { title: 'pages.home', icon: 'mdi-home', path: '/', order: 0 },
  { title: 'pages.inbounds', icon: 'mdi-cloud-download', path: '/inbounds', order: 10 },
  { title: 'pages.clients', icon: 'mdi-account-multiple', path: '/clients', order: 20 },
  { title: 'pages.outbounds', icon: 'mdi-cloud-upload', path: '/outbounds', order: 30 },
  { title: 'pages.endpoints', icon: 'mdi-cloud-tags', path: '/endpoints', order: 40 },
  { title: 'pages.services', icon: 'mdi-server', path: '/services', order: 50 },
  { title: 'pages.tls', icon: 'mdi-certificate', path: '/tls', order: 60 },
  { title: 'pages.rules', icon: 'mdi-routes', path: '/rules', order: 61 },
  { title: 'pages.dns', icon: 'mdi-dns', path: '/dns', order: 62 },
  { title: 'pages.singBoxConfig', icon: 'mdi-code-json', path: '/sing-box-config', order: 63 },
  { title: 'pages.admins', icon: 'mdi-account-tie', path: '/admins', order: 100 },
  { title: 'pages.settings', icon: 'mdi-cog', path: '/settings', order: 130 },
  { title: 'pages.support', icon: 'mdi-heart-outline', path: '/support', order: 140 },
]

const toClassicItem = (item: NavItem): ClassicMenuItem => ({
  title: item.title,
  icon: item.icon,
  path: item.path,
  order: item.order ?? 1000,
})

export const classicMenuItems = computed<ClassicMenuItem[]>(() =>
  [...coreClassicMenuItems, ...navItems.value.map(toClassicItem)]
    .sort((a, b) => a.order - b.order),
)
