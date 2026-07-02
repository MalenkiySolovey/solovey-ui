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
  })
}
