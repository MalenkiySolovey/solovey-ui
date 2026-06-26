<template>
  <div class="remote-subscription-profile">
    <div class="remote-subscription-profile__summary">
      <div class="remote-subscription-profile__summary-card">
        <span>Subscription</span>
        <strong>{{ data?.name || '-' }}</strong>
      </div>
      <div class="remote-subscription-profile__summary-card">
        <span>Source</span>
        <strong>{{ data?.url || '-' }}</strong>
      </div>
      <div class="remote-subscription-profile__summary-card">
        <span>Last successful update</span>
        <strong>{{ lastUpdated }}</strong>
      </div>
      <div class="remote-subscription-profile__summary-card">
        <span>Top-level blocks</span>
        <strong>{{ blocks.length }}</strong>
      </div>
      <div class="remote-subscription-profile__summary-card">
        <span>Groups</span>
        <strong>{{ groupCount }}</strong>
      </div>
      <div class="remote-subscription-profile__summary-card">
        <span>Stored connections</span>
        <strong>{{ connectionCount }}</strong>
      </div>
    </div>

    <v-alert
      v-if="data?.lastError"
      density="compact"
      type="error"
      variant="tonal"
    >
      {{ data.lastError }}
    </v-alert>

    <v-alert
      v-if="blocks.length === 0"
      density="compact"
      type="info"
      variant="tonal"
    >
      Internal subscription data has not been collected yet. Refresh the subscription to build a profile.
    </v-alert>

    <div v-else class="remote-subscription-profile__blocks">
      <RemoteProfileBlock
        v-for="(block, index) in blocks"
        :key="`${block.name}-${index}`"
        :block="block"
      />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import RemoteProfileBlock from './RemoteProfileBlock.vue'

interface ProfileValue {
  value: string
  sources?: string[]
}

interface ProfileCharacteristic {
  key: string
  label: string
  values?: ProfileValue[]
}

interface ProfileBlock {
  name: string
  type: string
  sources?: string[]
  characteristics?: ProfileCharacteristic[]
  connections?: ProfileBlock[]
}

interface CollectedSubscriptionData {
  name?: string
  url?: string
  lastUpdated?: number
  lastError?: string
  profile?: ProfileBlock[]
  connections?: unknown[]
}

const props = defineProps<{
  data: CollectedSubscriptionData | null
}>()

const blocks = computed(() => Array.isArray(props.data?.profile) ? props.data?.profile ?? [] : [])
const connectionCount = computed(() => Array.isArray(props.data?.connections) ? props.data?.connections.length : 0)
const groupCount = computed(() => blocks.value.filter(block => (block.connections?.length ?? 0) > 0).length)
const lastUpdated = computed(() => {
  const value = Number(props.data?.lastUpdated ?? 0)
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString()
})
</script>

<style scoped>
.remote-subscription-profile {
  display: grid;
  gap: 12px;
  max-height: 64vh;
  overflow: auto;
  padding-right: 4px;
}

.remote-subscription-profile__summary {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.remote-subscription-profile__summary-card {
  background: rgba(var(--v-theme-on-surface), .045);
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  display: grid;
  gap: 3px;
  min-width: 0;
  padding: 8px 10px;
}

.remote-subscription-profile__summary-card span {
  color: rgba(var(--v-theme-on-surface), .62);
  font-size: .74rem;
  text-transform: uppercase;
}

.remote-subscription-profile__summary-card strong {
  font-size: .9rem;
  overflow-wrap: anywhere;
}

.remote-subscription-profile__blocks {
  display: grid;
  gap: 8px;
}
</style>
