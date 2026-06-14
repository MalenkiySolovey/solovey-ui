<template>
  <component
    :is="EntityForm"
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :tags="outboundTags"
    @close="closeModal"
  />
  <OutboundBulk
    v-model="bulkModal.visible"
    :visible="bulkModal.visible"
    :outboundTags="outboundTags"
    @close="closeBulkModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />

  <OutboundsNexusList
    v-if="mode === 'nexus'"
    :outbounds="<any[]>orderedOutbounds"
    :onlines="onlines"
    :enable-traffic="enableTraffic"
    :check-results="checkResults"
    :order-dirty="outboundOrderDirty"
    :order-saving="outboundOrderSaving"
    :testing-all="testingAll"
    @add="showModal(0)"
    @add-bulk="showBulkModal"
    @cancel-order="cancelOutboundOrder"
    @del="delOutbound"
    @edit="showModal"
    @move="moveOutbound"
    @move-to="dragOutbound"
    @save-order="saveOutboundOrder"
    @sort-by-name="sortOutboundsByName"
    @stats="showStats"
    @test="checkOutbound"
    @test-all="checkAllOutbounds"
  />

  <template v-else>
    <v-row justify="center" align="center">
      <v-col cols="auto">
        <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn color="primary" @click="showBulkModal">{{ $t('actions.addbulk') }}</v-btn>
      </v-col>
      <v-col cols="auto">
        <ManualOrderControls
          :dirty="outboundOrderDirty"
          :saving="outboundOrderSaving"
          :sort-disabled="orderedOutbounds.length < 2"
          @cancel="cancelOutboundOrder"
          @save="saveOutboundOrder"
          @sort="sortOutboundsByName"
        />
      </v-col>
      <v-col cols="auto">
        <v-btn
          color="secondary"
          variant="outlined"
          :loading="testingAll"
          append-icon="mdi-speedometer"
          :disabled="testingAll || orderedOutbounds.length === 0"
          @click="checkAllOutbounds"
        >
          {{ $t('actions.testAll') || 'Test all' }}
        </v-btn>
      </v-col>
    </v-row>
    <v-row>
      <v-col
        cols="12"
        sm="4"
        md="3"
        lg="2"
        v-for="(item, index) in <any[]>orderedOutbounds"
        :key="item.tag"
        :draggable="false"
        @pointerdown="outboundDrag.prepare($event)"
        @dragstart="outboundDrag.start($event, item.id)"
        @dragover="outboundDrag.over($event)"
        @drop="onOutboundDrop($event, item.id)"
        @dragend="outboundDrag.clear($event)"
      >
        <v-card rounded="xl" elevation="5" min-width="200" :title="item.tag">
          <v-card-subtitle style="margin-top: -15px;">
            <v-row>
              <v-col>{{ item.type }}</v-col>
              <v-col v-if="item.remoteOutboundManaged">
                <v-chip color="info" density="compact" size="small" variant="tonal">
                  {{ $t('remoteOutbound.managedOutbound') }}
                </v-chip>
              </v-col>
            </v-row>
          </v-card-subtitle>
          <v-card-text>
            <v-row>
              <v-col>{{ $t('in.addr') }}</v-col>
              <v-col>
                {{ item.server?? '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('in.port') }}</v-col>
              <v-col>
                {{ item.server_port?? '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('objects.tls') }}</v-col>
              <v-col>
                {{ Object.hasOwn(item,'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : '-'  }}
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
            <v-row>
              <v-col>{{ $t('out.delay') }}</v-col>
              <v-col>
                <v-progress-circular
                  v-if="checkResults[item.tag]?.loading"
                  indeterminate
                  size="20"
                />
                <v-icon
                  icon="mdi-speedometer"
                  v-else
                  @click="checkOutbound(item.tag)"
                >
                  <v-tooltip activator="parent" location="top" :text="$t('actions.test')"></v-tooltip>
                </v-icon>
                <template v-if="checkResults[item.tag]?.loading == false">
                  <template v-if="checkResults[item.tag]">
                    <v-chip
                      v-if="checkResults[item.tag].success"
                      density="compact"
                      size="small"
                      color="success"
                      variant="flat"
                    >
                      {{ checkResults[item.tag].data?.Delay + $t('date.ms') }}
                    </v-chip>
                    <v-tooltip v-else location="top" :text="checkResults[item.tag].errorMessage || $t('failed')">
                      <template v-slot:activator="{ props }">
                        <v-icon v-bind="props" size="small" color="error" icon="mdi-close-circle" />
                      </template>
                    </v-tooltip>
                  </template>
                </template>
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
                <v-card-text>
                  <div>{{ $t('confirm') }}</div>
                  <div v-if="item.remoteOutboundManaged" class="outbounds__delete-hint">
                    {{ $t('remoteOutbound.deleteManagedOutboundWarning') }}
                  </div>
                </v-card-text>
                <v-card-actions>
                  <v-btn color="error" variant="outlined" @click="delOutbound(item.tag)">{{ $t('yes') }}</v-btn>
                  <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
                </v-card-actions>
              </v-card>
            </v-overlay>
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
import ManualOrderControls from '@/components/ManualOrderControls.vue'
import HttpUtils from '@/plugins/httputil'
import OutboundVue from '@/layouts/modals/Outbound.vue'
import OutboundBulk from '@/layouts/modals/OutboundBulk.vue'
import Stats from '@/layouts/modals/Stats.vue'
import { Outbound } from '@/types/outbounds'
import { computed, defineAsyncComponent, ref } from 'vue'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag } from '@/composables/useManualDrag'
import type { ManualSortDirection } from '@/composables/useManualReorder'
import { usePendingManualOrder } from '@/composables/usePendingManualOrder'
import { useAsyncTaskQueue } from '@/composables/useAsyncTaskQueue'

const { mode } = useUiMode()

const OutboundsNexusList = defineAsyncComponent(
  () => import('@/views/outbounds/OutboundsNexusList.vue'),
)
const OutboundDrawer = defineAsyncComponent(
  () => import('@/components/nexus/drawers/OutboundDrawer.vue'),
)

const EntityForm = computed(() => (mode.value === 'nexus' ? OutboundDrawer : OutboundVue))

interface CheckResult {
  loading?: boolean
  success: boolean
  data?: { OK?: boolean; Delay?: number; Error?: string } | null
  errorMessage?: string
}

const checkResults = ref<Record<string, CheckResult>>({})
const outboundCheckQueue = useAsyncTaskQueue(8)

const performOutboundCheck = async (tag: string) => {
  checkResults.value = { ...checkResults.value, [tag]: { loading: true, success: false } }
  const msg = await HttpUtils.get('api/checkOutbound', { tag })
  const success = msg.success && msg.obj?.OK
  const errorMessage = success ? undefined : (msg.obj?.Error ?? msg.msg ?? '')
  checkResults.value = {
    ...checkResults.value,
    [tag]: { loading: false, success, data: msg.obj ?? null, errorMessage }
  }
}

const checkOutbound = async (tag: string) => {
  await outboundCheckQueue.runOne(tag, () => performOutboundCheck(tag))
}

const testingAll = outboundCheckQueue.runningAll

const checkAllOutbounds = async () => {
  const list = outbounds.value
  if (list.length === 0) return
  await outboundCheckQueue.runMany(list, item => item.tag, item => performOutboundCheck(item.tag))
}

const outbounds = computed((): Outbound[] => {
  return <Outbound[]> Data().outbounds
})
const outboundsOrder = usePendingManualOrder<Outbound>('outbounds', outbounds)
const orderedOutbounds = outboundsOrder.displayItems
const outboundOrderDirty = outboundsOrder.dirty
const outboundOrderSaving = outboundsOrder.saving

const outboundTags = computed((): string[] => {
  return [...Data().outbounds?.map((o:Outbound) => o.tag), ...Data().endpoints?.map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.outbound?? []
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
  modal.value.data = id == 0 ? '' : JSON.stringify(outbounds.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const bulkModal = ref({ visible: false })

const showBulkModal = () => {
  bulkModal.value.visible = true
}

const closeBulkModal = () => {
  bulkModal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "outbound",
  tag: "",
})

const delOutbound = async (tag: string) => {
  const success = await Data().save("outbounds", "del", tag)
  if (success) delOverlay.value = []
}

const moveOutbound = (id: number, dir: number) => {
  outboundsOrder.move(id, dir)
}

const dragOutbound = (draggedId: number, targetId: number) => {
  outboundsOrder.moveTo(draggedId, targetId)
}

const sortOutboundsByName = (direction: ManualSortDirection) => {
  outboundsOrder.sortByText(direction, "tag")
}

const saveOutboundOrder = () => outboundsOrder.save()
const cancelOutboundOrder = () => outboundsOrder.reset()

const outboundDrag = useManualDrag<number>()
const onOutboundDrop = (event: DragEvent, targetId: number) => {
  outboundDrag.drop(event, targetId, dragOutbound)
}

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}
</script>

<style scoped>
.outbounds__delete-hint {
  color: rgb(var(--v-theme-warning));
  font-size: 0.82rem;
  line-height: 1.35;
  margin-top: 8px;
  max-width: 260px;
}
</style>
