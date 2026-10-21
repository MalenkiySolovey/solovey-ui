<template>
  <span class="host-context-label" data-testid="host-context" role="status">
    {{ $t('hostContext.label') }}: {{ hostID === LOCAL_HOST.id ? $t('hostContext.local') : $t('hostContext.unavailable') }}
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { LOCAL_HOST, routeHostID } from '@/router/hostContext'
const route = useRoute()
const hostID = computed(() => routeHostID(route))
</script>
<style scoped>
.host-context-label { flex: 0 0 auto; font-size: 0.8rem; margin-inline: 8px; white-space: nowrap; }
</style>
