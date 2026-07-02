import { registerComponent } from '@/componentSystem/registry'

export function register() {
  registerComponent({
    id: 'import-xui',
    apiVersion: '1.0',
    routes: [
      {
        path: '/migrate-xui',
        name: 'component.import-xui',
        meta: { title: 'migrateXui.title' },
        component: () => import('./views/MigrateXui.vue'),
      },
    ],
    slots: [
      {
        slot: 'backup-restore:actions',
        component: () => import('./BackupImportActions.vue'),
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
