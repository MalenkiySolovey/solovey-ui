import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const componentProfile = process.env.SOLOVEY_UI_PROFILE === 'core' ? 'core' : 'full'

export default defineConfig({
  plugins: [vue()],
  define: {
    __SOLOVEY_UI_COMPONENT_PROFILE__: JSON.stringify(componentProfile),
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.spec.ts', '../components/*/frontend/**/*.test.ts', '../components/*/frontend/**/*.spec.ts'],
    css: false,
    testTimeout: 30000,
    // Transform Vuetify through Vite (not Node's ESM loader) so its side-effect
    // .css imports are neutralised by `css: false` instead of crashing on the
    // unknown ".css" extension. Lets tests render real Vuetify components.
    server: {
      deps: {
        inline: ['vuetify'],
      },
    },
  },
})
