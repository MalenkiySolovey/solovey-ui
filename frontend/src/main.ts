import './uiMode/bootstrap'
import './styles/manual-drop.scss'

/**
 * main.ts
 *
 * Bootstraps Vuetify and other plugins then mounts the App`
 */

// Composables
import { createApp, ref, watch } from 'vue'

// Components
import App from './App.vue'

// Use router
import router from './router'

// Store
import store from './store'
import Data from '@/store/modules/data'
import { configureManualOrderPersistence } from '@/shared/dnd/manualReorder'

// Plugins
import { registerPlugins } from '@/plugins'

// Locale
import { configureLocaleExtensions, i18n, loadInitialLocaleMessages } from '@/locales'
import { syncEnabledComponents } from '@/componentSystem/loader'
import { loadActiveComponentLocaleMessages } from '@/componentSystem/locales'

// Notivue
import { createNotivue } from 'notivue'
import 'notivue/notification.css'
import 'notivue/animations.css'
const notivue = createNotivue({
  position: 'bottom-center',
  limit: 4,
  enqueue: false,
  avoidDuplicates: true,
  notifications: {
    global: {
      duration: 3000
    }
  },
})

const bootstrap = async () => {
  configureLocaleExtensions(loadActiveComponentLocaleMessages)
  await loadInitialLocaleMessages()

  const loading = ref(false)
  const app = createApp(App)
  app.provide('loading', loading)

  registerPlugins(app)

  app
    .use(store)
    .use(i18n)
    .use(notivue)

  const data = Data()
  configureManualOrderPersistence((object, order) => data.reorder(object, order))
  watch(
    () => data.components.map(component => `${component.id}:${component.installed}:${component.active}`).join('|'),
    () => {
      void syncEnabledComponents(router)
    },
  )

  app
    .use(router)
    .mount('#app')
}

void bootstrap()
