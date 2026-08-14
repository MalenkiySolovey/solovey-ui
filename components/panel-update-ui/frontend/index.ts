import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'panel-update-ui',
    apiVersion: '1.0',
    slots: [
      {
        slot: 'settings:maintenance',
        component: () => import('./components/PanelUpdateCard.vue'),
        order: 10,
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
