<template>
  <v-navigation-drawer
    v-model="showDrawer"
    :temporary="isMobile"
    :expand-on-hover="!isMobile"
    :rail="!isMobile"
    :permanent="!isMobile"
    @click="isMobile ? $emit('toggleDrawer') : null"
  >
    <v-list-item
      height="63"
      title="Solovey UI"
    >
      <template v-slot:prepend>
        <img aria-hidden="true" :src="logoUrl" class="default-drawer__logo" alt="" />
      </template>
      <template v-slot:append v-if="isMobile">
        <v-icon icon="mdi-close" />
      </template>
    </v-list-item>

    <v-divider></v-divider>

    <v-list density="compact" nav>
      <v-list-item link
        v-for="item in menu"
        :key="item.title"
        :to="item.path"
        :active="router.currentRoute.value.path == item.path">
        <template v-slot:prepend>
          <v-icon :icon="item.icon"></v-icon>
        </template>
        <v-list-item-title v-text="$t(item.title)"></v-list-item-title>
      </v-list-item>
    </v-list>
    <template v-slot:append>
      <v-list-item prepend-icon="mdi-logout" :title="$t('menu.logout')" @click="Logout"></v-list-item>
    </template>
  </v-navigation-drawer>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import router from '@/router'
import { logout } from '@/plugins/httputil'
import logoUrl from '@/assets/logo.png'

const props = defineProps(['isMobile','displayDrawer'])

const showDrawer = computed((): boolean => {
  return props.displayDrawer
})

const menu = [
  { title: 'pages.home', icon: 'mdi-home',  path: '/' },
  { title: 'pages.inbounds', icon: 'mdi-cloud-download',  path: '/inbounds' },
  { title: 'pages.clients', icon: 'mdi-account-multiple',  path: '/clients' },
  { title: 'pages.outbounds', icon: 'mdi-cloud-upload',  path: '/outbounds' },
  { title: 'pages.remoteOutboundSubscriptions', icon: 'mdi-cloud-download',  path: '/remote-subscriptions' },
  { title: 'pages.endpoints', icon: 'mdi-cloud-tags',  path: '/endpoints' },
  { title: 'pages.services', icon: 'mdi-server',  path: '/services' },
  { title: 'pages.tls', icon: 'mdi-certificate',  path: '/tls' },
  { title: 'pages.basics', icon: 'mdi-application-cog',  path: '/basics' },
  { title: 'pages.rules', icon: 'mdi-routes',  path: '/rules' },
  { title: 'pages.dns', icon: 'mdi-dns',  path: '/dns' },
  { title: 'pages.singBoxConfig', icon: 'mdi-code-json',  path: '/sing-box-config' },
  { title: 'pages.admins', icon: 'mdi-account-tie',  path: '/admins' },
  { title: 'pages.telegram', icon: 'mdi-send',  path: '/telegram' },
  { title: 'pages.paidSub', icon: 'mdi-cash-multiple',  path: '/paid-subscriptions' },
  { title: 'pages.audit', icon: 'mdi-shield-search',  path: '/audit' },
  { title: 'pages.diagnostics', icon: 'mdi-clipboard-search',  path: '/diagnostics' },
  { title: 'pages.settings', icon: 'mdi-cog',  path: '/settings' },
]

const Logout = async () => {
  logout()
}
</script>

<style scoped>
.default-drawer__logo {
  background: transparent;
  border-radius: 6px;
  display: block;
  height: 32px;
  object-fit: contain;
  width: 32px;
}
</style>
