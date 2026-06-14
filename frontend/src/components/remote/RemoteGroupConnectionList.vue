<template>
  <div class="remote-group-connection-list">
    <v-text-field
      :model-value="search"
      density="compact"
      hide-details
      prepend-inner-icon="mdi-magnify"
      :label="searchLabel"
      variant="outlined"
      @update:model-value="emit('update:search', String($event ?? ''))"
    />

    <div class="remote-group-connection-list__rows">
      <div
        v-for="connection in filteredConnections"
        :key="connection.id"
        class="remote-group-connection-list__row"
      >
        <v-checkbox-btn
          density="compact"
          :model-value="selectedSet.has(connection.id)"
          :disabled="loading"
          @update:model-value="(checked) => emit('toggle', connection.id, Boolean(checked))"
        />
        <span class="remote-group-connection-list__text">
          <strong>{{ connection.name }}</strong>
          <small>{{ connection.type }} / {{ connection.outboundTag }}</small>
        </span>
      </div>

      <div v-if="filteredConnections.length === 0" class="remote-group-connection-list__empty">
        {{ emptyText }}
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

interface RemoteGroupConnection {
  id: number
  name: string
  type: string
  outboundTag: string
}

const props = withDefaults(defineProps<{
  connections: RemoteGroupConnection[]
  selectedIds: number[]
  search?: string
  searchLabel: string
  emptyText: string
  loading?: boolean
}>(), {
  search: '',
  loading: false,
})

const emit = defineEmits<{
  'update:search': [value: string]
  toggle: [connectionId: number, checked: boolean]
}>()

const selectedSet = computed(() => new Set(props.selectedIds))

const filteredConnections = computed(() => {
  const query = props.search.trim().toLowerCase()
  if (!query) return props.connections

  return props.connections.filter(connection => [
    connection.name,
    connection.outboundTag,
    connection.type,
  ].some(value => String(value ?? '').toLowerCase().includes(query)))
})
</script>

<style scoped>
.remote-group-connection-list {
  display: grid;
  gap: 6px;
}

.remote-group-connection-list__rows {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 6px;
  max-height: 180px;
  overflow: auto;
}

.remote-group-connection-list__row {
  align-items: center;
  display: flex;
  gap: 6px;
  min-height: 34px;
  padding: 3px 8px;
}

.remote-group-connection-list__row + .remote-group-connection-list__row {
  border-top: 1px solid rgba(var(--v-border-color), calc(var(--v-border-opacity) * .7));
}

.remote-group-connection-list__row:hover {
  background: rgba(var(--v-theme-on-surface), .04);
}

.remote-group-connection-list__text {
  display: grid;
  min-width: 0;
  user-select: text;
}

.remote-group-connection-list__text strong,
.remote-group-connection-list__text small {
  overflow-wrap: anywhere;
}

.remote-group-connection-list__text strong {
  font-size: .88rem;
  line-height: 1.25;
}

.remote-group-connection-list__text small {
  color: rgba(var(--v-theme-on-surface), .62);
  font-size: .74rem;
  line-height: 1.2;
}

.remote-group-connection-list__empty {
  color: rgba(var(--v-theme-on-surface), .62);
  padding: 10px;
  text-align: center;
}
</style>
