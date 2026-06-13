<template>
  <div class="admins-nexus">
    <page-header :subtitle="subtitle" :title="$t('pages.admins')" />

    <page-toolbar>
      <template #actions>
        <ManualSortButton
          :disabled="users.length < 2"
          @sort="sortByName"
        />
        <v-btn color="primary" prepend-icon="lucide:plus" variant="flat" @click="emit('add')">
          {{ $t('admin.addAdmin') }}
        </v-btn>
        <v-btn prepend-icon="lucide:history" variant="text" @click="emit('changes', '')">
          {{ $t('admin.changes') }}
        </v-btn>
        <v-btn prepend-icon="lucide:key-round" variant="text" @click="emit('token')">
          {{ $t('admin.api.token') }}
        </v-btn>
        <v-btn color="error" prepend-icon="lucide:log-out" variant="text" @click="confirmLogoutAll">
          {{ $t('admin.logoutAll') }}
        </v-btn>
      </template>
    </page-toolbar>

    <nexus-data-table
      :columns="columns"
      draggable-rows
      :items="users"
      :row-key="(item) => item.id"
      @row-drop="(dragged, target) => emit('moveTo', dragged.id, target.id)"
    >
      <template #col.username="{ item }">
        <span class="admins-nexus__name">{{ item.username }}</span>
      </template>

      <template #col.ip="{ item }">
        <span v-if="item.ip" class="nexus-mono">{{ item.ip }}</span>
        <span v-else class="admins-nexus__muted">—</span>
      </template>

      <template #actions="{ item }">
        <row-actions :actions="adminActions(item)" @action="(key) => handleAction(key, item)" />
      </template>

      <template #empty>
        <empty-state icon="lucide:user-cog" :title="$t('table.noData')" />
      </template>
    </nexus-data-table>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

import ManualSortButton from '@/components/ManualSortButton.vue'
import type { Column } from '@/components/nexus/data/dataTableColumns'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import type { RowAction } from '@/components/nexus/data/rowActions'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useI18n } from 'vue-i18n'
import type { ManualSortDirection } from '@/composables/useManualReorder'

interface AdminRow {
  id: number
  username: string
  loginDate: string
  loginTime: string
  ip: string
  isCurrent: boolean
  [key: string]: unknown
}

const props = defineProps<{
  users: AdminRow[]
}>()

const emit = defineEmits<{
  add: []
  edit: [user: AdminRow]
  changes: [actor: string]
  del: [user: AdminRow]
  move: [id: number, dir: number]
  moveTo: [draggedId: number, targetId: number]
  sortByName: [direction: ManualSortDirection]
  token: []
  logoutAll: []
}>()

const { t } = useI18n()
const { confirm } = useConfirm()

const sortByName = (direction: ManualSortDirection) => {
  emit('sortByName', direction)
}

const subtitle = computed(() => t('nexus.summary.admins', { total: props.users.length }))

const columns: Column<AdminRow>[] = [
  { key: 'username', labelKey: 'admin.username' },
  { key: 'loginDate', labelKey: 'admin.date' },
  { key: 'loginTime', labelKey: 'admin.time' },
  { key: 'ip', labelKey: 'IP' },
]

// Named <entity>Actions (not rowActions) so it never shadows the RowActions
// component in this template — see the Nexus list convention.
const adminActions = (item: AdminRow): RowAction[] => [
  { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: props.users.findIndex(row => row.id === item.id) === 0 },
  { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: props.users.findIndex(row => row.id === item.id) === props.users.length - 1 },
  { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
  { key: 'changes', labelKey: 'admin.changes', icon: 'lucide:history', inline: true },
  { key: 'del', labelKey: 'admin.deleteAdmin', icon: 'lucide:trash-2', tone: 'error', inline: true, hidden: item.isCurrent },
]

const handleAction = (key: string, item: AdminRow) => {
  switch (key) {
    case 'up':
      emit('move', item.id, -1)
      break
    case 'down':
      emit('move', item.id, 1)
      break
    case 'edit':
      emit('edit', item)
      break
    case 'changes':
      emit('changes', item.username)
      break
    case 'del':
      // Deletion is password-protected via AdminDeleteModal (kept by the parent),
      // so this routes to that modal rather than the lightweight confirm dialog.
      emit('del', item)
      break
  }
}

const confirmLogoutAll = async () => {
  const ok = await confirm({
    title: t('admin.logoutAll'),
    message: t('admin.logoutAllConfirm'),
    confirmLabel: t('admin.logoutAll'),
    tone: 'error',
  })

  if (ok) emit('logoutAll')
}

defineExpose({ usersCount: () => props.users.length })
</script>

<style scoped>
.admins-nexus__name {
  color: var(--nexus-text-primary);
  font-weight: 600;
}

.admins-nexus__muted {
  color: var(--nexus-text-muted);
}
</style>
