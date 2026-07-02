import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'paid-subscriptions',
    apiVersion: '1.0',
    routes: [
      {
        path: '/paid-subscriptions',
        name: 'component.paid-subscriptions',
        meta: { title: 'paidSub.title' },
        component: () => import('./views/PaidSubscriptions.vue'),
      },
    ],
    nav: [
      {
        title: 'paidSub.title',
        icon: 'mdi-cash-multiple',
        nexusIcon: 'lucide:credit-card',
        path: '/paid-subscriptions',
        order: 80,
        section: 'integrations',
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
