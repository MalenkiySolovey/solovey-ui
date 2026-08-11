import { registerComponent, unregisterComponent } from '@/componentSystem/registry'

const componentID = 'server-protection'

export function register() {
  registerComponent({
    id: componentID,
    apiVersion: '1.0',
    routes: [
      {
        path: '/server-protection',
        name: 'component.server-protection',
        meta: { title: 'serverProtection.title' },
        component: () => import('./views/ServerProtection.vue'),
      },
    ],
    nav: [
      {
        title: 'serverProtection.title',
        icon: 'mdi-shield-lock-outline',
        nexusIcon: 'lucide:shield-check',
        path: '/server-protection',
        order: 123,
        section: 'system',
      },
    ],
    slots: [
      {
        slot: 'inbound:editor',
        component: () => import('./views/InboundProtectionEditor.vue'),
        order: 220,
      },
      {
        slot: 'inbound:status-detail',
        component: () => import('./views/InboundProtectionStatus.vue'),
        order: 220,
      },
    ],
    locales: {
      en: () => import('./locales/en'),
      ru: () => import('./locales/ru'),
    },
  })
}

export function unregister() {
  unregisterComponent(componentID)
}
