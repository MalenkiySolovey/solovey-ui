import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'telegram',
    apiVersion: '1.0',
    routes: [
      {
        path: '/telegram',
        name: 'component.telegram.settings',
        meta: { title: 'telegram.title' },
        component: () => import('./views/TelegramSettings.vue'),
      },
    ],
    nav: [
      {
        title: 'telegram.title',
        icon: 'mdi-send',
        nexusIcon: 'lucide:send',
        path: '/telegram',
        order: 70,
        section: 'integrations',
      },
    ],
    slots: [
      {
        slot: 'backup-restore:backup-options',
        component: () => import('./BackupEncryptionOption.vue'),
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
