import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'observability-extra',
    apiVersion: '1.0',
    routes: [
      {
        path: '/audit',
        name: 'component.observability-extra.audit',
        meta: { title: 'audit.title' },
        component: () => import('./views/Audit.vue'),
      },
      {
        path: '/diagnostics',
        name: 'component.observability-extra.diagnostics',
        meta: { title: 'diagnostics.title' },
        component: () => import('./views/Diagnostics.vue'),
      },
    ],
    nav: [
      {
        title: 'audit.title',
        icon: 'mdi-shield-search',
        nexusIcon: 'lucide:file-text',
        path: '/audit',
        order: 120,
        section: 'system',
      },
      {
        title: 'diagnostics.title',
        icon: 'mdi-clipboard-search',
        nexusIcon: 'lucide:gauge',
        path: '/diagnostics',
        order: 121,
        section: 'system',
      },
    ],
    locales: {
      en: () => import('./locales/en'),
      fa: () => import('./locales/fa'),
      vi: () => import('./locales/vi'),
      zhHans: () => import('./locales/zhcn'),
      zhHant: () => import('./locales/zhtw'),
      ru: () => import('./locales/ru'),
    },
  })
}
