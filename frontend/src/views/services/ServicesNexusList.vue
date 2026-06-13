<template>
  <div class="services-nexus">
    <page-header
      :search="search"
      searchable
      :subtitle="subtitle"
      :title="$t('pages.services')"
      @update:search="search = $event"
    />

    <page-toolbar>
      <template #actions>
        <ManualSortButton
          :disabled="services.length < 2"
          @sort="sortByName"
        />
        <v-btn color="primary" prepend-icon="lucide:plus" variant="flat" @click="emit('add')">
          {{ $t('actions.add') }}
        </v-btn>
      </template>
    </page-toolbar>

    <nexus-data-table
      :columns="columns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="filtered"
      :row-key="(item) => item.id"
      @row-drop="(dragged, target) => emit('moveTo', dragged.id, target.id)"
    >
      <template #col.tag="{ item }">
        <span class="services-nexus__tag">{{ item.tag }}</span>
      </template>
      <template #col.listen="{ item }">
        <span v-if="item.type !== 'oom-killer' && item.listen" class="nexus-mono">{{ item.listen }}</span>
        <span v-else class="services-nexus__muted">—</span>
      </template>
      <template #col.listen_port="{ item }">
        <span v-if="item.type !== 'oom-killer' && item.listen_port" class="nexus-mono">{{ item.listen_port }}</span>
        <span v-else class="services-nexus__muted">—</span>
      </template>
      <template #col.tls="{ item }">
        <nexus-badge
          v-if="item.type !== 'oom-killer'"
          :label="(item.tls_id ?? 0) > 0 ? $t('nexus.on') : $t('nexus.off')"
          :variant="(item.tls_id ?? 0) > 0 ? 'success' : 'secondary'"
        />
        <span v-else class="services-nexus__muted">—</span>
      </template>
      <template #col.memory="{ item }">
        <span>{{ item.type === 'oom-killer' ? (item.memory_limit || '—') : '—' }}</span>
      </template>

      <template #actions="{ item }">
        <row-actions :actions="serviceActions(item)" @action="(key) => handleAction(key, item)" />
      </template>

      <template #empty>
        <empty-state icon="lucide:server" :title="$t('table.noData')" />
      </template>
    </nexus-data-table>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ManualSortButton from '@/components/ManualSortButton.vue'
import type { Column } from '@/components/nexus/data/dataTableColumns'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import type { RowAction } from '@/components/nexus/data/rowActions'
import NexusBadge from '@/components/nexus/primitives/Badge.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import type { ManualSortDirection } from '@/composables/useManualReorder'

interface ServiceRow {
  id: number
  tag: string
  type: string
  listen?: string
  listen_port?: number
  tls_id?: number
  memory_limit?: string
  [key: string]: unknown
}

const props = defineProps<{ services: ServiceRow[] }>()

const emit = defineEmits<{
  add: []
  edit: [id: number]
  del: [id: number]
  move: [id: number, dir: number]
  moveTo: [draggedId: number, targetId: number]
  sortByName: [direction: ManualSortDirection]
}>()

const { t } = useI18n()
const { confirm } = useConfirm()
const search = ref('')

const sortByName = (direction: ManualSortDirection) => {
  emit('sortByName', direction)
}

const subtitle = computed(() => t('nexus.summary.services', { total: props.services.length }))

const columns: Column<ServiceRow>[] = [
  { key: 'tag', labelKey: 'objects.tag' },
  { key: 'type', labelKey: 'type' },
  { key: 'listen', labelKey: 'in.addr' },
  { key: 'listen_port', labelKey: 'in.port' },
  { key: 'tls', labelKey: 'objects.tls' },
  { key: 'memory', labelKey: 'types.oom.memoryLimit' },
]

const filtered = computed<ServiceRow[]>(() => {
  const query = search.value.trim().toLowerCase()

  if (!query) return props.services

  return props.services.filter(item => String(item.tag).toLowerCase().includes(query))
})

const serviceActions = (item: ServiceRow): RowAction[] => [
  { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || props.services.findIndex(row => row.id === item.id) === 0 },
  { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || props.services.findIndex(row => row.id === item.id) === props.services.length - 1 },
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', divider: true },
]

const handleAction = async (key: string, item: ServiceRow) => {
  if (key === 'up') {
    emit('move', item.id, -1)
    return
  }
  if (key === 'down') {
    emit('move', item.id, 1)
    return
  }
  if (key === 'edit') {
    emit('edit', item.id)
    return
  }

  const discard = await confirm({
    title: `${t('actions.del')} ${t('objects.service')}`,
    message: item.tag,
    confirmLabel: t('actions.del'),
    tone: 'error',
  })

  if (discard) emit('del', item.id)
}
</script>

<style scoped>
.services-nexus__tag {
  color: var(--nexus-text-primary);
  font-weight: 600;
}

.services-nexus__muted {
  color: var(--nexus-text-muted);
}
</style>
