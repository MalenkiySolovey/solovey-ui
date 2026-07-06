// Plugins
import vue from '@vitejs/plugin-vue'
import vuetify, { transformAssetUrls } from 'vite-plugin-vuetify'

// Utilities
import fs from 'node:fs'
import path from 'node:path'
import { defineConfig, normalizePath, type Plugin } from 'vite'
import { fileURLToPath, URL } from 'node:url'

const isE2E = process.env.SUI_E2E === '1'
const componentProfile = process.env.SOLOVEY_UI_PROFILE === 'core' ? 'core' : 'full'
const frontendRoot = fileURLToPath(new URL('.', import.meta.url))
const repoRoot = path.resolve(frontendRoot, '..')
const bundledComponentIDs = resolveBundledComponentIDs()
const e2eWebPath = normalizeWebPath(process.env.SUI_E2E_WEB_PATH || '/phase6-panel/')
const apiProxyPath = isE2E ? `${e2eWebPath}api` : '/app/api'

export default defineConfig({
  base: '',
  plugins: [
    e2eBaseURLPlugin(),
    componentEntriesPlugin(bundledComponentIDs),
    vue({
      template: { transformAssetUrls },
    }),
    vuetify({
      autoImport: true,
      styles: {
        configFile: 'src/styles/settings.scss',
      },
    })
  ],
  build: {
    manifest: true,
    outDir: 'dist',
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        // Prefix the bare Rolldown hash so a filename can never START with "_".
        // The hash uses URL-safe base64 (A-Za-z0-9-_), so a bare "[hash].js"
        // occasionally begins with "_", and Go's `//go:embed` excludes files
        // whose names start with "_" or "." (unless the `all:` prefix is used).
        // Such a chunk was silently dropped from the embedded binary -> 404 on a
        // dynamically imported module -> blank panel. (web/web.go also uses
        // `all:` now as a belt-and-suspenders safeguard.)
        entryFileNames: 'assets/app-[hash].js',
        chunkFileNames: 'assets/chunk-[hash].js',
        assetFileNames: (assetInfo) => {
          if (assetInfo.names.some(name => name.endsWith('.css')))
            return 'assets/style-[hash].css'
          return 'assets/[name][extname]'
        },
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('/vue/') || id.includes('/vue-router/') || id.includes('/pinia/') || id.includes('/vue-i18n/')) return 'vendor-vue'
          if (id.includes('/vuetify/')) return 'vendor-vuetify'
          if (id.includes('/axios/')) return 'vendor-http'
          return undefined
        },
      },
    }
  },
  define: {
    'process.env': {},
    __SOLOVEY_UI_COMPONENT_PROFILE__: JSON.stringify(componentProfile),
    // vue-i18n / @intlify compile-time feature flags. Required because vue-i18n is
    // excluded from dep pre-bundling below (its esm-bundler build reads these guards
    // directly in the browser); also silences the flag warnings in the production build.
    __VUE_I18N_FULL_INSTALL__: true,
    __VUE_I18N_LEGACY_API__: false,
    __INTLIFY_PROD_DEVTOOLS__: false,
    __INTLIFY_JIT_COMPILATION__: false,
    __INTLIFY_DROP_MESSAGE_COMPILER__: false,
  },
  optimizeDeps: {
    // vue-i18n is excluded because Rolldown's dep optimizer mis-bundles it on this
    // toolchain (rolldown-vite 8 + Node 25): the optimized chunk references
    // `init_runtime_dom_esm_bundler` (the @vue/runtime-dom esm-bundler init) without
    // defining it, so `app.use(i18n)` throws `ReferenceError: init_runtime_dom_esm_bundler
    // is not defined` and the whole SPA fails to mount (blank login page). Serving it as
    // native ESM via the flags above avoids the broken pre-bundle.
    exclude: ['vuetify', 'vuetify/components', 'vuetify/directives', 'vue-i18n'],
  },
  resolve: {
    alias: [
      { find: '@', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
      { find: /^notivue$/, replacement: fileURLToPath(new URL('./node_modules/notivue/dist/index.js', import.meta.url)) },
      { find: /^vue$/, replacement: fileURLToPath(new URL('./node_modules/vue/dist/vue.runtime.esm-bundler.js', import.meta.url)) },
      { find: /^vue-i18n$/, replacement: fileURLToPath(new URL('./node_modules/vue-i18n/dist/vue-i18n.mjs', import.meta.url)) },
      { find: /^vue-router$/, replacement: fileURLToPath(new URL('./node_modules/vue-router/dist/vue-router.js', import.meta.url)) },
      { find: /^vue3-persian-datetime-picker$/, replacement: fileURLToPath(new URL('./node_modules/vue3-persian-datetime-picker/dist/vue3-persian-datetime-picker.common.js', import.meta.url)) },
      { find: 'vuetify/components', replacement: fileURLToPath(new URL('./node_modules/vuetify/lib/components', import.meta.url)) },
      { find: 'vuetify/directives', replacement: fileURLToPath(new URL('./node_modules/vuetify/lib/directives', import.meta.url)) },
      { find: 'vuetify/iconsets', replacement: fileURLToPath(new URL('./node_modules/vuetify/lib/iconsets', import.meta.url)) },
    ],
    extensions: ['.js', '.json', '.jsx', '.mjs', '.ts', '.tsx', '.vue'],
  },
  server: {
    fs: {
      allow: [
        fileURLToPath(new URL('..', import.meta.url)),
      ],
    },
    hmr: isE2E ? false : undefined,
    port: 3000,
    proxy: {
      [apiProxyPath]: {
        target: 'http://localhost:2095',
        changeOrigin: true,
      },
    },
  }
})

function componentEntriesPlugin(componentIDs: string[]): Plugin {
  const moduleID = 'virtual:solovey-component-entries'
  const resolvedModuleID = '\0' + moduleID

  return {
    name: 'solovey-component-entries',
    resolveId(id) {
      return id === moduleID ? resolvedModuleID : undefined
    },
    load(id) {
      if (id !== resolvedModuleID) return undefined
      const entries = componentIDs
        .map(componentID => {
          const entry = componentFrontendEntry(componentID)
          if (!fs.existsSync(entry)) return undefined
          const specifier = `/@fs/${normalizePath(entry)}`
          return `  ${JSON.stringify(componentID)}: () => import(${JSON.stringify(specifier)})`
        })
        .filter((entry): entry is string => Boolean(entry))
      return `export const componentEntries = {\n${entries.join(',\n')}\n}\n`
    },
  }
}

function e2eBaseURLPlugin(): Plugin {
  return {
    name: 'solovey-e2e-base-url',
    transformIndexHtml(html) {
      if (!isE2E) return html
      return html.replace('{{ .BASE_URL }}', e2eWebPath)
    },
  }
}

function resolveBundledComponentIDs() {
  if (componentProfile === 'core') return []

  const raw = process.env.SOLOVEY_UI_COMPONENT_IDS
  if (raw !== undefined) {
    return parseComponentIDList(raw)
  }
  return discoverFrontendComponentIDs()
}

function parseComponentIDList(raw: string) {
  const ids = raw
    .split(',')
    .map(id => id.trim())
    .filter(Boolean)
  const unique = [...new Set(ids)]
  for (const id of unique) {
    assertComponentID(id)
    const manifest = path.join(repoRoot, 'components', id, 'component.json')
    if (!fs.existsSync(manifest)) {
      throw new Error(`Unknown Solovey UI component id: ${id}`)
    }
  }
  return unique.sort((a, b) => a.localeCompare(b))
}

function discoverFrontendComponentIDs() {
  const componentsRoot = path.join(repoRoot, 'components')
  if (!fs.existsSync(componentsRoot)) return []
  return fs.readdirSync(componentsRoot, { withFileTypes: true })
    .filter(entry => entry.isDirectory())
    .map(entry => entry.name)
    .filter(id => {
      assertComponentID(id)
      return fs.existsSync(path.join(componentsRoot, id, 'component.json')) &&
        fs.existsSync(componentFrontendEntry(id))
    })
    .sort((a, b) => a.localeCompare(b))
}

function componentFrontendEntry(componentID: string) {
  return path.join(repoRoot, 'components', componentID, 'frontend', 'index.ts')
}

function assertComponentID(id: string) {
  if (!/^[a-z0-9-]+$/.test(id)) {
    throw new Error(`Invalid Solovey UI component id: ${id}`)
  }
}

function normalizeWebPath(value: string) {
  const trimmed = String(value || '').trim()
  if (!trimmed || trimmed === '/') return '/phase6-panel/'
  return `${trimmed.startsWith('/') ? '' : '/'}${trimmed}${trimmed.endsWith('/') ? '' : '/'}`
}
