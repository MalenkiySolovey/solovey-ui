/// <reference types="vite/client" />

declare const __SOLOVEY_UI_COMPONENT_PROFILE__: 'full' | 'core'

declare module 'virtual:solovey-component-entries' {
  const componentEntries: Record<string, () => Promise<{ register: () => void }>>
  export { componentEntries }
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}
