import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'remote-outbound-subscriptions',
    apiVersion: '1.0',
    routes: [
      {
        path: '/remote-subscriptions',
        name: 'component.remote-outbound-subscriptions',
        meta: { title: 'remoteOutbound.title' },
        alias: '/remote-outbound-subscriptions',
        component: () => import('./views/RemoteOutboundSubscriptions.vue'),
      },
    ],
    nav: [
      {
        title: 'remoteOutbound.title',
        icon: 'mdi-cloud-download',
        nexusIcon: 'lucide:cloud-download',
        path: '/remote-subscriptions',
        order: 35,
        section: 'proxy',
        singBoxSettings: true,
      },
    ],
    slots: [
      {
        slot: 'settings:subscription-advanced',
        component: () => import('./components/RemoteSubscriptionSettings.vue'),
        order: 10,
      },
      {
        slot: 'client-editor:subscription-sources',
        component: () => import('./components/RemoteClientSubscriptionSources.vue'),
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
