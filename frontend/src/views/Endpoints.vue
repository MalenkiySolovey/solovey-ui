<template>
  <EndpointVue
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :tags="endpointTags"
    @close="closeModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :data="qrcode.data"
    @close="closeQrCode"
  />

  <EndpointsNexusList
    v-if="mode === 'nexus'"
    :endpoints="<any[]>endpoints"
    :onlines="onlines"
    :enable-traffic="enableTraffic"
    @add="showModal(0)"
    @del="delEndpoint"
    @del-many="delEndpointsBulk"
    @edit="showModal"
    @move="moveEndpoint"
    @move-many-to="dragSelectedEndpoints"
    @move-to="dragEndpoint"
    @sort-by-name="sortEndpointsByName"
    @qr="showQrCode"
    @stats="showStats"
  />

  <template v-else>
    <v-row>
      <v-col cols="12" justify="center" align="center">
        <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
        <ManualSortButton
          :disabled="endpoints.length < 2"
          style="margin: 0 5px;"
          @sort="sortEndpointsByName"
        />
        <BulkSelectionControls
          :active="endpointSelectMode"
          :count="selectedEndpointCount"
          inactive-color="secondary"
          inactive-variant="outlined"
          style="margin: 0 5px;"
          @delete="deleteSelectedEndpoints"
          @toggle="toggleEndpointSelectMode"
        />
      </v-col>
    </v-row>
    <v-row>
      <v-col
        cols="12"
        sm="4"
        md="3"
        lg="2"
        v-for="(item, index) in <any[]>endpoints"
        :key="item.tag"
        class="manual-drop-grid-cell"
        :class="endpointDrag.indicatorClasses(item.id)"
        :style="endpointDrag.indicatorStyles(item.id)"
        :draggable="false"
        @pointerdown="endpointDrag.prepare($event)"
        @dragstart="endpointDrag.start($event, item.id)"
        @dragover="endpointDrag.overTarget($event, item.id, endpoints.map(row => row.id), endpointSelectMode ? selectedEndpointIds.map(Number) : [], false, 'grid')"
        @dragleave="endpointDrag.leaveTarget($event, item.id)"
        @drop="onEndpointDrop($event, item.id)"
        @dragend="endpointDrag.clear($event)"
      >
        <v-card
          rounded="xl"
          elevation="5"
          min-width="200"
          :title="item.tag"
          class="endpoints__card"
          :class="{ 'endpoints__card--selected': isEndpointSelected(item.id) }"
        >
          <div v-if="endpointSelectMode" class="endpoints__select manual-drag-no-drag">
            <v-checkbox-btn
              :model-value="isEndpointSelected(item.id)"
              :aria-label="$t('table.selectRow')"
              density="compact"
              @update:model-value="toggleEndpointSelection(item.id, Boolean($event))"
            />
          </div>
          <v-card-subtitle style="margin-top: -15px;">
            <v-row>
              <v-col>{{ item.type }}</v-col>
            </v-row>
          </v-card-subtitle>
          <v-card-text>
            <v-row>
              <v-col>{{ $t('in.addr') }}</v-col>
              <v-col>
                {{ item.address?.length>0 ? item.address[0] : '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('in.port') }}</v-col>
              <v-col>
                {{ item.listen_port>0 ? item.listen_port : '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('types.wg.peers') }}</v-col>
              <v-col>
                {{ item.peers?.length?? '-'  }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('online') }}</v-col>
              <v-col>
                <template v-if="onlines.includes(item.tag)">
                  <v-chip density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
                </template>
                <template v-else>-</template>
              </v-col>
            </v-row>
          </v-card-text>
          <v-divider></v-divider>
          <v-card-actions style="padding: 0;">
            <v-btn icon="mdi-file-edit" @click="showModal(item.id)">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
            </v-btn>
            <v-btn icon="mdi-file-remove" style="margin-inline-start:0;" color="warning" @click="delOverlay[index] = true">
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
                  <v-btn color="error" variant="outlined" @click="delEndpoint(item.tag)">{{ $t('yes') }}</v-btn>
                  <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
                </v-card-actions>
              </v-card>
            </v-overlay>
            <v-icon
            class="me-2"
            v-if="item.type == 'wireguard' && item.peers?.length>0"
            @click="showQrCode(item.id)"
          >
            mdi-qrcode
          </v-icon>
            <v-btn icon="mdi-chart-line" @click="showStats(item.tag)" v-if="Data().enableTraffic">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </template>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ManualSortButton from '@/components/ManualSortButton.vue'
import EndpointVue from '@/layouts/modals/Endpoint.vue'
import Stats from '@/layouts/modals/Stats.vue'
import QrCode from '@/layouts/modals/WgQrCode.vue'
import { Endpoint } from '@/types/endpoints'
import { computed, defineAsyncComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
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

const EndpointsNexusList = defineAsyncComponent(
  () => import('@/views/endpoints/EndpointsNexusList.vue'),
)

const endpoints = computed((): Endpoint[] => {
  return <Endpoint[]> Data().endpoints
})
const endpointSelection = useBulkSelection(endpoints, item => item.id)
const endpointSelectMode = endpointSelection.active
const selectedEndpointIds = endpointSelection.selectedIds
const selectedEndpointCount = endpointSelection.selectedCount
const isEndpointSelected = endpointSelection.isSelected
const toggleEndpointSelection = endpointSelection.toggle
const toggleEndpointSelectMode = endpointSelection.toggleActive

const endpointTags = computed((): any[] => {
  return endpoints.value?.map((o:Endpoint) => o.tag)
})

const onlines = computed(() => {
  return [...Data().onlines.inbound?? [], ...Data().onlines.outbound??[] ]
})

const enableTraffic = computed((): boolean => {
  return Data().enableTraffic
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(endpoints.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "endpoint",
  tag: "",
})

const delEndpoint = async (tag: string) => {
  const index = endpoints.value.findIndex(i => i.tag == tag)
  const success = await Data().save("endpoints", "del", tag)
  if (success) delOverlay.value[index] = false
}

const delEndpointsBulk = async (tags: string[]) => {
  const uniqueTags = [...new Set(tags.map(String).filter(Boolean))]
  let success = true
  for (const tag of uniqueTags) {
    success = await Data().save("endpoints", "del", tag)
    if (!success) break
  }
  if (success) {
    delOverlay.value = []
    endpointSelection.clear()
  }
  return success
}

const deleteSelectedEndpoints = async () => {
  const rows = endpointSelection.selectedItems.value
  if (rows.length === 0) return
  const accepted = await confirm({
    title: `${t('actions.delbulk')} ${t('objects.endpoint')}`,
    message: rows.map(item => item.tag).join('\n'),
    confirmLabel: t('actions.del'),
    tone: 'error',
  })
  if (!accepted) return
  await delEndpointsBulk(rows.map(item => item.tag))
}

const moveEndpoint = async (id: number, dir: number) => {
  await moveManualOrder("endpoints", endpoints.value as any[], id, dir)
}

const dragEndpoint = async (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
  await dragManualOrder("endpoints", endpoints.value as any[], draggedId, targetId, "id", position)
}

const dragSelectedEndpoints = async (draggedIds: number[], targetId: number, position: ManualDropPosition | null = null) => {
  await moveManyManualOrder("endpoints", endpoints.value as any[], draggedIds, targetId, "id", position)
}

const sortEndpointsByName = async (direction: ManualSortDirection) => {
  await sortManualOrderByText("endpoints", endpoints.value as any[], direction, "tag")
}

const endpointDrag = useManualDrag<number>()
const onEndpointDrop = (event: DragEvent, targetId: number) => {
  endpointDrag.drop(event, targetId, (draggedId, dropTargetId, position) => {
    if (endpointSelectMode.value && endpointSelection.isSelected(draggedId)) {
      void dragSelectedEndpoints(endpointSelection.selectedIds.value.map(Number), dropTargetId, position)
      return
    }
    void dragEndpoint(draggedId, dropTargetId, position)
  })
}

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}

const qrcode = ref({
  visible: false,
  data: <any>{},
})

const showQrCode = (id: number) => {
  qrcode.value.data = endpoints.value.findLast(o => o.id == id)
  qrcode.value.visible = true
}
const closeQrCode = () => {
  qrcode.value.visible = false
}
</script>

<style scoped>
.endpoints__card {
  position: relative;
}

.endpoints__card--selected {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}

.endpoints__select {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 2;
}
</style>
