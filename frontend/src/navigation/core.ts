export type NavigationSection = 'dashboard' | 'proxy' | 'network' | 'integrations' | 'system'
export type NavigationCountKey = 'inbounds' | 'clients' | 'outbounds' | 'endpoints' | 'services' | 'tlsConfigs'
export interface CoreNavigationItem {
  title: string
  icon: string
  nexusIcon: string
  path: string
  order: number
  section: NavigationSection
  countKey?: NavigationCountKey
  singBoxSettings?: boolean
  nexusContributionsAfter?: boolean
}

// Product destinations live here. Layouts project these facts; optional
// components contribute through their registry, never through this list.
export const coreNavigation: readonly CoreNavigationItem[] = [
  { title: 'pages.home', icon: 'mdi-home', nexusIcon: 'lucide:layout-grid', path: '/', order: 0, section: 'dashboard' },
  { title: 'pages.inbounds', icon: 'mdi-cloud-download', nexusIcon: 'lucide:zap', path: '/inbounds', order: 10, section: 'proxy', countKey: 'inbounds', singBoxSettings: true },
  { title: 'pages.clients', icon: 'mdi-account-multiple', nexusIcon: 'lucide:users', path: '/clients', order: 20, section: 'proxy', countKey: 'clients' },
  { title: 'pages.outbounds', icon: 'mdi-cloud-upload', nexusIcon: 'lucide:arrow-up-right', path: '/outbounds', order: 30, section: 'proxy', countKey: 'outbounds', singBoxSettings: true, nexusContributionsAfter: true },
  { title: 'pages.endpoints', icon: 'mdi-cloud-tags', nexusIcon: 'lucide:globe', path: '/endpoints', order: 40, section: 'proxy', countKey: 'endpoints', singBoxSettings: true },
  { title: 'pages.services', icon: 'mdi-server', nexusIcon: 'lucide:server', path: '/services', order: 50, section: 'proxy', countKey: 'services', singBoxSettings: true },
  { title: 'pages.tls', icon: 'mdi-certificate', nexusIcon: 'lucide:lock', path: '/tls', order: 60, section: 'network', countKey: 'tlsConfigs', singBoxSettings: true },
  { title: 'pages.rules', icon: 'mdi-routes', nexusIcon: 'lucide:list', path: '/rules', order: 61, section: 'network', singBoxSettings: true },
  { title: 'pages.dns', icon: 'mdi-dns', nexusIcon: 'lucide:network', path: '/dns', order: 62, section: 'network', singBoxSettings: true },
  { title: 'pages.singBoxConfig', icon: 'mdi-code-json', nexusIcon: 'lucide:file-text', path: '/sing-box-config', order: 63, section: 'network', singBoxSettings: true },
  { title: 'pages.admins', icon: 'mdi-account-tie', nexusIcon: 'lucide:user-cog', path: '/admins', order: 100, section: 'system', nexusContributionsAfter: true },
  { title: 'pages.security', icon: 'mdi-account-lock-outline', nexusIcon: 'lucide:shield-check', path: '/security', order: 110, section: 'system' },
  { title: 'pages.sshManagement', icon: 'mdi-server-network', nexusIcon: 'lucide:terminal', path: '/ssh-management', order: 120, section: 'system' },
  { title: 'pages.deployment', icon: 'mdi-server-security', nexusIcon: 'lucide:server-cog', path: '/deployment', order: 125, section: 'system' },
  { title: 'pages.operations', icon: 'mdi-shield-sync-outline', nexusIcon: 'lucide:activity', path: '/operations', order: 127, section: 'system' },
  { title: 'pages.settings', icon: 'mdi-cog', nexusIcon: 'lucide:settings', path: '/settings', order: 130, section: 'system' },
  { title: 'pages.support', icon: 'mdi-heart-outline', nexusIcon: 'lucide:heart-handshake', path: '/support', order: 140, section: 'system' },
]
