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
        :active="route.path == item.path">
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
import { useRoute } from 'vue-router'
import { logout } from '@/shared/composables/useAuthOperations'
import logoUrl from '@/assets/logo.png'
import { classicMenuItems } from '@/componentSystem/navigation'

const props = defineProps(['isMobile','displayDrawer'])
const route = useRoute()

const showDrawer = computed((): boolean => {
  return props.displayDrawer
})

const menu = computed(() => classicMenuItems.value)

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
