<template>
  <component :is="ShellComponent">
    <router-view v-if="localHost" :key="hostID" />
    <v-alert v-else type="warning" variant="tonal" class="ma-4">{{ $t('hostContext.localOnly') }}</v-alert>
  </component>
</template>

<script lang="ts" setup>
import { computed, defineAsyncComponent } from 'vue'
import { useRoute } from 'vue-router'
import { LOCAL_HOST, routeHostID } from '@/router/hostContext'
const route = useRoute()
const hostID = computed(() => routeHostID(route))
const localHost = computed(() => hostID.value === LOCAL_HOST.id)

import { useUiMode } from '@/uiMode/useUiMode'

const ClassicShell = defineAsyncComponent(
  () => import('@/layouts/default/Default.vue'),
)
const NexusShell = defineAsyncComponent(
  () => import('@/layouts/nexus/NexusShell.vue'),
)

const { mode } = useUiMode()
const ShellComponent = computed(() =>
  mode.value === 'nexus' ? NexusShell : ClassicShell,
)
</script>
