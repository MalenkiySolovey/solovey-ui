<template>
  <v-navigation-drawer
    :key="temporary ? 'mobile' : 'desktop'"
    class="nexus-sidebar"
    :expand-on-hover="rail && !temporary"
    :location="rtl ? 'right' : 'left'"
    :model-value="open"
    :permanent="!temporary"
    :rail="rail && !temporary"
    :temporary="temporary"
    @update:model-value="emit('update:open', $event)"
  >
    <v-list-item
      class="nexus-sidebar__brand"
      height="64"
      prepend-avatar="@/assets/logo.svg"
      title="S-UI"
    >
      <template #append v-if="temporary">
        <v-btn
          :aria-label="$t('actions.close')"
          icon="mdi-close"
          variant="text"
          @click="emit('update:open', false)"
        />
      </template>
    </v-list-item>

    <v-divider />

    <v-list class="nexus-sidebar__navigation" nav>
      <v-list-item
        v-for="item in menu"
        :key="item.path"
        link
        :prepend-icon="item.icon"
        :title="$t(item.title)"
        :to="item.path"
        @click="emit('navigate')"
      />
    </v-list>

    <template #append>
      <div class="nexus-sidebar__footer">
        <v-divider />
        <v-list nav>
          <v-list-item
            prepend-icon="mdi-logout"
            :title="$t('menu.logout')"
            @click="logout"
          />
        </v-list>
        <slot name="footer" />
      </div>
    </template>
  </v-navigation-drawer>
</template>

<script lang="ts" setup>
import { logout } from '@/plugins/httputil'

defineProps<{
  open: boolean
  rail: boolean
  rtl: boolean
  temporary: boolean
}>()

const emit = defineEmits<{
  navigate: []
  'update:open': [value: boolean]
}>()

const menu = [
  { title: 'pages.home', icon: 'mdi-home', path: '/' },
  { title: 'pages.inbounds', icon: 'mdi-cloud-download', path: '/inbounds' },
  { title: 'pages.clients', icon: 'mdi-account-multiple', path: '/clients' },
  { title: 'pages.outbounds', icon: 'mdi-cloud-upload', path: '/outbounds' },
  { title: 'pages.endpoints', icon: 'mdi-cloud-tags', path: '/endpoints' },
  { title: 'pages.services', icon: 'mdi-server', path: '/services' },
  { title: 'pages.tls', icon: 'mdi-certificate', path: '/tls' },
  { title: 'pages.basics', icon: 'mdi-application-cog', path: '/basics' },
  { title: 'pages.rules', icon: 'mdi-routes', path: '/rules' },
  { title: 'pages.dns', icon: 'mdi-dns', path: '/dns' },
  { title: 'pages.admins', icon: 'mdi-account-tie', path: '/admins' },
  { title: 'pages.telegram', icon: 'mdi-send', path: '/telegram' },
  { title: 'pages.audit', icon: 'mdi-shield-search', path: '/audit' },
  { title: 'pages.settings', icon: 'mdi-cog', path: '/settings' },
]
</script>

<style scoped>
.nexus-sidebar {
  background: var(--nexus-surface-1);
  border-color: var(--nexus-border);
}

.nexus-sidebar__brand {
  letter-spacing: 0;
}

.nexus-sidebar__navigation {
  padding-inline: var(--nexus-gap-2);
}

.nexus-sidebar__footer {
  background: var(--nexus-surface-1);
  border-block-start: 1px solid var(--nexus-border);
  padding-block-end: var(--nexus-gap-2);
}

.nexus-sidebar__footer .v-divider {
  margin-block-end: var(--nexus-gap-1);
}
</style>
