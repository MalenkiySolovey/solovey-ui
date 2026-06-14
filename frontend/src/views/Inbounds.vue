<template>
  <component
    :is="EntityForm"
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :inTags="inTags"
    :tlsConfigs="tlsConfigs"
    @close="closeModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />

  <InboundsNexusList
    v-if="mode === 'nexus'"
    :inbounds="<any[]>orderedInbounds"
    :onlines="onlines"
    :enable-traffic="enableTraffic"
    :order-dirty="inboundOrderDirty"
    :order-saving="inboundOrderSaving"
    @add="showModal(0)"
    @cancel-order="cancelInboundOrder"
    @clone="clone"
    @del="delInbound"
    @edit="showModal"
    @move="moveInbound"
    @move-to="dragInbound"
    @save-order="saveInboundOrder"
    @sort-by-name="sortInboundsByName"
    @stats="showStats"
  />

  <template v-else>
    <v-row>
      <v-col cols="12" justify="center" align="center">
        <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
        <ManualOrderControls
          :dirty="inboundOrderDirty"
          :saving="inboundOrderSaving"
          :sort-disabled="orderedInbounds.length < 2"
          style="margin: 0 5px;"
          @cancel="cancelInboundOrder"
          @save="saveInboundOrder"
          @sort="sortInboundsByName"
        />
      </v-col>
    </v-row>
    <v-row>
      <v-col
        cols="12"
        sm="4"
        md="3"
        lg="2"
        v-for="(item, index) in <any[]>orderedInbounds"
        :key="item.tag"
        :draggable="false"
        @pointerdown="inboundDrag.prepare($event)"
        @dragstart="inboundDrag.start($event, item.id)"
        @dragover="inboundDrag.over($event)"
        @drop="onInboundDrop($event, item.id)"
        @dragend="inboundDrag.clear($event)"
      >
        <v-card rounded="xl" elevation="5" min-width="200" :title="item.tag">
          <v-card-subtitle style="margin-top: -15px;">
            <v-row>
              <v-col>{{ item.type }}</v-col>
            </v-row>
          </v-card-subtitle>
          <v-card-text>
            <v-row>
              <v-col>{{ $t('in.addr') }}</v-col>
              <v-col>
                {{ item.listen }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('in.port') }}</v-col>
              <v-col>
                {{ item.listen_port }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('objects.tls') }}</v-col>
              <v-col>
                {{ item.tls_id > 0 ? $t('enable') : $t('disable') }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('pages.clients') }}</v-col>
              <v-col>
                <template v-if="item.users">
                  <v-tooltip activator="parent" dir="ltr" location="bottom" v-if="item.users.length > 0">
                    <span v-for="u in item.users">{{ u }}<br /></span>
                  </v-tooltip>
                  {{ item.users.length }}
                </template>
                <template v-else>-</template>
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
                  <v-btn color="error" variant="outlined" @click="delInbound(item.id)">{{ $t('yes') }}</v-btn>
                  <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
                </v-card-actions>
              </v-card>
            </v-overlay>
            <v-btn icon="mdi-content-duplicate" :loading="cloneLoading" @click="clone(item.id)">
              <v-icon />
              <v-tooltip activator="parent" location="top" :text="$t('actions.clone')"></v-tooltip>
            </v-btn>
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
import InboundVue from '@/layouts/modals/Inbound.vue'
import Stats from '@/layouts/modals/Stats.vue'
import { Config } from '@/types/config'
import { computed, defineAsyncComponent, ref } from 'vue'
import { createInbound, Inbound } from '@/types/inbounds'
import RandomUtil from '@/plugins/randomUtil'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag } from '@/composables/useManualDrag'
import type { ManualSortDirection } from '@/composables/useManualReorder'
import { usePendingManualOrder } from '@/composables/usePendingManualOrder'

const { mode } = useUiMode()

const InboundsNexusList = defineAsyncComponent(
  () => import('@/views/inbounds/InboundsNexusList.vue'),
)
const InboundDrawer = defineAsyncComponent(
  () => import('@/components/nexus/drawers/InboundDrawer.vue'),
)

// Same props/emit contract for both shells; only the chrome differs.
const EntityForm = computed(() => (mode.value === 'nexus' ? InboundDrawer : InboundVue))

const appConfig = computed((): Config => {
  return <Config> Data().config
})

const inbounds = computed((): Inbound[] => {
  return <Inbound[]> Data().inbounds
})
const inboundsOrder = usePendingManualOrder<Inbound>('inbounds', inbounds)
const orderedInbounds = inboundsOrder.displayItems
const inboundOrderDirty = inboundsOrder.dirty
const inboundOrderSaving = inboundsOrder.saving

const tlsConfigs = computed((): any[] => {
  return <any[]> Data().tlsConfigs
})

const inTags = computed((): string[] => {
  return [...inbounds.value?.map(i => i.tag), ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.inbound?? []
})

const enableTraffic = computed((): boolean => {
  return Data().enableTraffic
})

const modal = ref({
  visible: false,
  id: 0,
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const delInbound = async (id: number) => {
  const index = inbounds.value.findIndex(i => i.id == id)
  const tag = inbounds.value[index].tag

  const success = await Data().save("inbounds", "del", tag)
  if (success) delOverlay.value = []
}

const moveInbound = (id: number, dir: number) => {
  inboundsOrder.move(id, dir)
}

const dragInbound = (draggedId: number, targetId: number) => {
  inboundsOrder.moveTo(draggedId, targetId)
}

const sortInboundsByName = (direction: ManualSortDirection) => {
  inboundsOrder.sortByText(direction, "tag")
}

const saveInboundOrder = () => inboundsOrder.save()
const cancelInboundOrder = () => inboundsOrder.reset()

const inboundDrag = useManualDrag<number>()
const onInboundDrop = (event: DragEvent, targetId: number) => {
  inboundDrag.drop(event, targetId, dragInbound)
}

let cloneLoading = ref(false)

const clone = async (id: number) => {
  cloneLoading.value = true
  const inboundArray = await Data().loadInbounds([id])
  const inbound = inboundArray[0]
  let newTag = inbound.type + "-" + RandomUtil.randomSeq(3)
  const newInbound = createInbound(inbound.type, { ...inbound,
    id: 0,
    tag: newTag,
    listen_port: RandomUtil.randomIntRange(10000, 60000),
  })
  await Data().save("inbounds", "new", newInbound)
  cloneLoading.value = false
}

const stats = ref({
  visible: false,
  resource: "inbound",
  tag: "",
})

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}
</script>
