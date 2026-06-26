<template>
  <div class="endpoints-nexus">
    <page-header
      :search="search"
      searchable
      :subtitle="subtitle"
      :title="$t('pages.endpoints')"
      @update:search="search = $event"
    />

    <page-toolbar>
      <template #actions>
        <ManualSortButton
          :disabled="endpoints.length < 2"
          @sort="sortByName"
        />
        <v-btn color="primary" prepend-icon="lucide:plus" variant="flat" @click="emit('add')">
          {{ $t('actions.add') }}
        </v-btn>
        <BulkSelectionControls
          :active="selectionMode"
          :count="selectedIds.length"
          @delete="deleteSelected"
          @toggle="toggleSelectionMode"
        />
      </template>
    </page-toolbar>

    <nexus-data-table
      :columns="columns"
      :drag-disabled="search.trim().length > 0"
      draggable-rows
      :items="filtered"
      :row-key="(item) => item.id"
      :selectable="selectionMode"
      :selected="selectedIds"
      @update:selected="selectedIds = $event"
      @row-drop="(dragged, target, position) => emit('moveTo', dragged.id, target.id, position)"
      @rows-drop="(dragged, target, position) => emit('moveManyTo', dragged.map(item => item.id), target.id, position)"
    >
      <template #col.status="{ item }">
        <status-badge v-if="onlines.includes(item.tag)" :label="$t('online')" tone="success" />
        <status-badge v-else :label="$t('nexus.status.offline')" tone="neutral" />
      </template>
      <template #col.tag="{ item }">
        <span class="endpoints-nexus__tag">{{ item.tag }}</span>
      </template>
      <template #col.address="{ item }">
        <span v-if="item.address && item.address.length" class="nexus-mono">{{ item.address[0] }}</span>
        <span v-else class="endpoints-nexus__muted">—</span>
      </template>
      <template #col.listen_port="{ item }">
        <span v-if="(item.listen_port ?? 0) > 0" class="nexus-mono">{{ item.listen_port }}</span>
        <span v-else class="endpoints-nexus__muted">—</span>
      </template>
      <template #col.peers="{ item }">
        <span>{{ item.peers?.length ?? '—' }}</span>
      </template>

      <template #actions="{ item }">
        <row-actions :actions="endpointActions(item)" @action="(key) => handleAction(key, item)" />
      </template>

      <template #empty>
        <empty-state icon="lucide:globe" :title="$t('table.noData')" />
      </template>
    </nexus-data-table>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ManualSortButton from '@/components/ManualSortButton.vue'
import type { Column } from '@/components/nexus/data/dataTableColumns'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import type { RowAction } from '@/components/nexus/data/rowActions'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useBulkSelection } from '@/shared/composables/dragSelection/bulkSelection'
import type { ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import type { ManualSortDirection } from '@/shared/composables/dragSelection/manualReorder'

interface EndpointRow {
  id: number
  tag: string
  type: string
  address?: string[]
  listen_port?: number
  peers?: unknown[]
  [key: string]: unknown
}

const props = defineProps<{
  endpoints: EndpointRow[]
  onlines: string[]
  enableTraffic: boolean
}>()

const emit = defineEmits<{
  add: []
  edit: [id: number]
  del: [tag: string]
  delMany: [tags: string[]]
  move: [id: number, dir: number]
  moveManyTo: [draggedIds: number[], targetId: number, position: ManualDropPosition | null]
  moveTo: [draggedId: number, targetId: number, position: ManualDropPosition | null]
  sortByName: [direction: ManualSortDirection]
  stats: [tag: string]
  qr: [id: number]
}>()

const { t } = useI18n()
const { confirm } = useConfirm()
const search = ref('')
const selection = useBulkSelection(computed(() => props.endpoints), item => item.id)
const selectionMode = selection.active
const selectedIds = selection.selectedIds

const sortByName = (direction: ManualSortDirection) => {
  emit('sortByName', direction)
}

const subtitle = computed(() => {
  const total = props.endpoints.length
  const online = props.endpoints.filter(item => props.onlines.includes(item.tag)).length

  return t('nexus.summary.endpoints', { total, online })
})

const columns: Column<EndpointRow>[] = [
  { key: 'status', labelKey: 'status' },
  { key: 'tag', labelKey: 'objects.tag' },
  { key: 'type', labelKey: 'type' },
  { key: 'address', labelKey: 'in.addr' },
  { key: 'listen_port', labelKey: 'in.port' },
  { key: 'peers', labelKey: 'types.wg.peers' },
]

const filtered = computed<EndpointRow[]>(() => {
  const query = search.value.trim().toLowerCase()

  if (!query) return props.endpoints

  return props.endpoints.filter(item => String(item.tag).toLowerCase().includes(query))
})

const selectedRows = selection.selectedItems
const toggleSelectionMode = selection.toggleActive

const deleteSelected = async () => {
  const rows = selectedRows.value
  if (rows.length === 0) return
  const accepted = await confirm({
    title: `${t('actions.delbulk')} ${t('objects.endpoint')}`,
    message: rows.map(item => item.tag).join('\n'),
    confirmLabel: t('actions.del'),
    tone: 'error',
  })
  if (!accepted) return
  emit('delMany', rows.map(item => item.tag))
  selection.clear()
}

const endpointActions = (item: EndpointRow): RowAction[] => [
  { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || props.endpoints.findIndex(row => row.id === item.id) === 0 },
  { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || props.endpoints.findIndex(row => row.id === item.id) === props.endpoints.length - 1 },
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'qr', labelKey: 'objects.config', icon: 'lucide:qr-code', inline: true, hidden: !(item.type === 'wireguard' && (item.peers?.length ?? 0) > 0) },
  { key: 'stats', labelKey: 'stats.graphTitle', icon: 'lucide:line-chart', inline: true, hidden: !props.enableTraffic },
  { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', divider: true },
]

const handleAction = async (key: string, item: EndpointRow) => {
  switch (key) {
    case 'up':
      emit('move', item.id, -1)
      break
    case 'down':
      emit('move', item.id, 1)
      break
    case 'edit':
      emit('edit', item.id)
      break
    case 'qr':
      emit('qr', item.id)
      break
    case 'stats':
      emit('stats', item.tag)
      break
    case 'del': {
      const discard = await confirm({
        title: `${t('actions.del')} ${t('objects.endpoint')}`,
        message: item.tag,
        confirmLabel: t('actions.del'),
        tone: 'error',
      })

      if (discard) emit('del', item.tag)
      break
    }
  }
}
</script>

<style scoped>
.endpoints-nexus__tag {
  color: var(--nexus-text-primary);
  font-weight: 600;
}

.endpoints-nexus__muted {
  color: var(--nexus-text-muted);
}
</style>
