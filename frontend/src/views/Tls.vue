<template>
  <component
    :is="EntityForm"
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    @close="closeModal"
    @save="saveModal"
  />

  <TlsNexusList
    v-if="mode === 'nexus'"
    :tls-configs="<any[]>tlsConfigs"
    :inbounds="<any[]>inbounds"
    @add="showModal(0)"
    @clone="clone"
    @del="delTls"
    @del-many="delTlsBulk"
    @edit="showModal"
    @move="moveTls"
    @move-many-to="dragSelectedTls"
    @move-to="dragTls"
    @sort-by-name="sortTlsByName"
  />

  <template v-else>
    <v-row>
      <v-col cols="12" justify="center" align="center">
        <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
        <ManualSortButton
          :disabled="tlsConfigs.length < 2"
          style="margin: 0 5px;"
          @sort="sortTlsByName"
        />
        <BulkSelectionControls
          :active="tlsSelectMode"
          :count="selectedTlsCount"
          inactive-color="secondary"
          inactive-variant="outlined"
          style="margin: 0 5px;"
          @delete="deleteSelectedTls"
          @toggle="toggleTlsSelectMode"
        />
      </v-col>
    </v-row>
    <v-row>
      <v-col
        cols="12"
        sm="4"
        md="3"
        lg="2"
        v-for="(item, index) in <any[]>tlsConfigs"
        :key="item.id"
        class="manual-drop-grid-cell"
        :class="tlsDrag.indicatorClasses(item.id)"
        :style="tlsDrag.indicatorStyles(item.id)"
        :draggable="false"
        @pointerdown="tlsDrag.prepare($event)"
        @dragstart="tlsDrag.start($event, item.id)"
        @dragover="tlsDrag.overTarget($event, item.id, tlsConfigs.map(row => row.id), tlsSelectMode ? selectedTlsIds.map(Number) : [], false, 'grid')"
        @dragleave="tlsDrag.leaveTarget($event, item.id)"
        @drop="onTlsDrop($event, item.id)"
        @dragend="tlsDrag.clear($event)"
      >
        <v-card
          rounded="xl"
          elevation="5"
          min-width="200"
          :title="item.name"
          class="tls__card"
          :class="{ 'tls__card--selected': isTlsSelected(item.id) }"
        >
          <div v-if="tlsSelectMode" class="tls__select manual-drag-no-drag">
            <v-checkbox-btn
              :model-value="isTlsSelected(item.id)"
              :aria-label="$t('table.selectRow')"
              density="compact"
              @update:model-value="toggleTlsSelection(item.id, Boolean($event))"
            />
          </div>
          <v-card-subtitle style="margin-top: -15px;">
            {{ item.server?.server_name?.length>0 ? item.server.server_name : "-" }}
          </v-card-subtitle>
          <v-card-text>
            <v-row>
              <v-col>{{ $t('pages.inbounds') }}</v-col>
              <v-col>
                <template v-if="tlsInbounds(item.id).length>0">
                  <v-tooltip activator="parent" dir="ltr" location="bottom">
                    <span v-for="i in tlsInbounds(item.id)">{{ i }}<br /></span>
                  </v-tooltip>
                  {{ tlsInbounds(item.id).length }}
                </template>
                <template v-else>-</template>
              </v-col>
            </v-row>
            <v-row>
              <v-col>ACME</v-col>
              <v-col>
                {{ $t(item.server?.acme == undefined ? 'no' : 'yes') }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>ECH</v-col>
              <v-col>
                {{ $t(item.server?.ech == undefined ? 'no' : 'yes') }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>Reality</v-col>
              <v-col>
                {{ $t(item.server?.reality == undefined ? 'no' : 'yes') }}
              </v-col>
            </v-row>
          </v-card-text>
          <v-divider></v-divider>
          <v-card-actions style="padding: 0;">
            <v-btn icon="mdi-file-edit" @click="showModal(item.id)">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
            </v-btn>
            <v-btn v-if="tlsInbounds(item.id).length == 0" icon="mdi-file-remove" style="margin-inline-start:0;" color="warning" @click="delOverlay[index] = true">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
            </v-btn>
            <v-overlay
              v-model="delOverlay[index]"
              contained
              class="align-center justify-center"
            >
              <v-card :title="$t('actions.del')" rounded="lg">
                <v-divider></v-divider>
                <v-card-text>{{ $t('confirm') }}</v-card-text>
                <v-card-actions>
                  <v-btn color="error" variant="outlined" @click="delTls(item.id)">{{ $t('yes') }}</v-btn>
                  <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
                </v-card-actions>
              </v-card>
            </v-overlay>
            <v-btn icon="mdi-content-duplicate" @click="clone(item)">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('actions.clone')"></v-tooltip>
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </template>
</template>

<script lang="ts" setup>
import TlsVue from '@/layouts/modals/Tls.vue'
import Data from '@/store/modules/data'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ManualSortButton from '@/components/ManualSortButton.vue'
import { computed, defineAsyncComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Inbound } from '@/types/inbounds'
import { tls } from '@/types/tls'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import {
  dragManualOrder,
  type ManualSortDirection,
  moveManyManualOrder,
  moveManualOrder,
  sortManualOrderByText,
} from '@/shared/composables/dragSelection/manualReorder'
import { useBulkSelection } from '@/shared/composables/dragSelection/bulkSelection'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'

const { mode } = useUiMode()
const { t } = useI18n()
const { confirm } = useConfirm()

const TlsNexusList = defineAsyncComponent(
  () => import('@/views/tls/TlsNexusList.vue'),
)
const TlsDrawer = defineAsyncComponent(
  () => import('@/components/nexus/drawers/TlsDrawer.vue'),
)

const EntityForm = computed(() => (mode.value === 'nexus' ? TlsDrawer : TlsVue))

const tlsConfigs = computed((): any[] => {
  return Data().tlsConfigs
})
const tlsSelection = useBulkSelection(tlsConfigs, item => item.id)
const tlsSelectMode = tlsSelection.active
const selectedTlsIds = tlsSelection.selectedIds
const selectedTlsCount = tlsSelection.selectedCount
const isTlsSelected = tlsSelection.isSelected
const toggleTlsSelection = tlsSelection.toggle
const toggleTlsSelectMode = tlsSelection.toggleActive

const inbounds = computed((): Inbound[] => {
  return Data().inbounds
})

const tlsInbounds = (id: number): string[] => {
  return inbounds.value.filter(i => i.tls_id == id).map(i => i.tag)
}

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

const delOverlay = ref(new Array<boolean>(tlsConfigs.value.length).fill(false))

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '{}' : JSON.stringify(tlsConfigs.value.findLast(t => t.id == id))
  modal.value.visible = true
}
const clone = (obj: any) => {
  let data = JSON.parse(JSON.stringify(obj))
  data.id = 0
  while (tlsConfigs.value.findIndex(t => t.name == data.name) != -1){
    data.name += "-copy"
  }
  saveModal(data)
}
const closeModal = () => {
  modal.value.visible = false
}
const saveModal = async (data:tls) => {
  const success = await Data().save("tls", data.id > 0 ? "edit" : "new", data)
  if (success) modal.value.visible = false
}

const delTls = async (id: number) => {
  const index = tlsConfigs.value.findIndex(t => t.id == id)
  const success = await Data().save("tls", "del", id)
  if (success) delOverlay.value[index] = false
}

const delTlsBulk = async (ids: number[]) => {
  const uniqueIds = [...new Set(ids.map(Number).filter(Boolean))]
  let success = true
  for (const id of uniqueIds) {
    if (tlsInbounds(id).length > 0) continue
    success = await Data().save("tls", "del", id)
    if (!success) break
  }
  if (success) {
    delOverlay.value = []
    tlsSelection.clear()
  }
  return success
}

const deleteSelectedTls = async () => {
  const rows = tlsSelection.selectedItems.value.filter(item => tlsInbounds(item.id).length === 0)
  if (rows.length === 0) return
  const accepted = await confirm({
    title: `${t('actions.delbulk')} ${t('objects.tls')}`,
    message: rows.map(item => item.name).join('\n'),
    confirmLabel: t('actions.del'),
    tone: 'error',
  })
  if (!accepted) return
  await delTlsBulk(rows.map(item => item.id))
}

const moveTls = async (id: number, dir: number) => {
  await moveManualOrder("tls", tlsConfigs.value as any[], id, dir)
}

const dragTls = async (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
  await dragManualOrder("tls", tlsConfigs.value as any[], draggedId, targetId, "id", position)
}

const dragSelectedTls = async (draggedIds: number[], targetId: number, position: ManualDropPosition | null = null) => {
  await moveManyManualOrder("tls", tlsConfigs.value as any[], draggedIds, targetId, "id", position)
}

const sortTlsByName = async (direction: ManualSortDirection) => {
  await sortManualOrderByText("tls", tlsConfigs.value as any[], direction, "name")
}

const tlsDrag = useManualDrag<number>()
const onTlsDrop = (event: DragEvent, targetId: number) => {
  tlsDrag.drop(event, targetId, (draggedId, dropTargetId, position) => {
    if (tlsSelectMode.value && tlsSelection.isSelected(draggedId)) {
      void dragSelectedTls(tlsSelection.selectedIds.value.map(Number), dropTargetId, position)
      return
    }
    void dragTls(draggedId, dropTargetId, position)
  })
}
</script>

<style scoped>
.tls__card {
  position: relative;
}

.tls__card--selected {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}

.tls__select {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 2;
}
</style>
