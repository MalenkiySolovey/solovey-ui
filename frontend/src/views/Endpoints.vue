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
    @edit="showModal"
    @move="moveEndpoint"
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
        :draggable="false"
        @pointerdown="endpointDrag.prepare($event)"
        @dragstart="endpointDrag.start($event, item.id)"
        @dragover="endpointDrag.over($event)"
        @drop="onEndpointDrop($event, item.id)"
        @dragend="endpointDrag.clear($event)"
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
import ManualSortButton from '@/components/ManualSortButton.vue'
import EndpointVue from '@/layouts/modals/Endpoint.vue'
import Stats from '@/layouts/modals/Stats.vue'
import QrCode from '@/layouts/modals/WgQrCode.vue'
import { Endpoint } from '@/types/endpoints'
import { computed, defineAsyncComponent, ref } from 'vue'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag } from '@/composables/useManualDrag'
import {
  dragManualOrder,
  type ManualSortDirection,
  moveManualOrder,
  sortManualOrderByText,
} from '@/composables/useManualReorder'

const { mode } = useUiMode()

const EndpointsNexusList = defineAsyncComponent(
  () => import('@/views/endpoints/EndpointsNexusList.vue'),
)

const endpoints = computed((): Endpoint[] => {
  return <Endpoint[]> Data().endpoints
})

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

const moveEndpoint = async (id: number, dir: number) => {
  await moveManualOrder("endpoints", endpoints.value as any[], id, dir)
}

const dragEndpoint = async (draggedId: number, targetId: number) => {
  await dragManualOrder("endpoints", endpoints.value as any[], draggedId, targetId)
}

const sortEndpointsByName = async (direction: ManualSortDirection) => {
  await sortManualOrderByText("endpoints", endpoints.value as any[], direction, "tag")
}

const endpointDrag = useManualDrag<number>()
const onEndpointDrop = (event: DragEvent, targetId: number) => {
  endpointDrag.drop(event, targetId, dragEndpoint)
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
