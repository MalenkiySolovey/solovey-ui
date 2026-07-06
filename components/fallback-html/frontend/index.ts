import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'fallback-html',
    apiVersion: '1.0',
    routes: [
      {
        path: '/fallback-html',
        name: 'component.fallback-html',
        meta: { title: 'fallbackHtml.title' },
        component: () => import('./views/PublicSite.vue'),
      },
    ],
    nav: [
      {
        title: 'fallbackHtml.title',
        icon: 'mdi-home',
        nexusIcon: 'lucide:globe',
        path: '/fallback-html',
        order: 122,
        section: 'integrations',
      },
    ],
    locales: {
      en: () => import('./locales/en'),
      ru: () => import('./locales/ru'),
    },
  })
}
