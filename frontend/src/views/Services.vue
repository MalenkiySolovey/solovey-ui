<template>
  <component
    :is="EntityForm"
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :inTags="inTags"
    :tsTags="tsTags"
    :ssTags="ssTags"
    :tlsConfigs="tlsConfigs"
    @close="closeModal"
  />

  <ServicesNexusList
    v-if="mode === 'nexus'"
    :services="<any[]>services"
    @add="showModal(0)"
    @del="delSrv"
    @del-many="delServicesBulk"
    @edit="showModal"
    @move="moveSrv"
    @move-many-to="dragSelectedServices"
    @move-to="dragSrv"
    @sort-by-name="sortServicesByName"
  />

  <template v-else>
    <v-row>
      <v-col cols="12" justify="center" align="center">
        <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
        <ManualSortButton
          :disabled="services.length < 2"
          style="margin: 0 5px;"
          @sort="sortServicesByName"
        />
        <BulkSelectionControls
          :active="serviceSelectMode"
          :count="selectedServiceCount"
          inactive-color="secondary"
          inactive-variant="outlined"
          style="margin: 0 5px;"
          @delete="deleteSelectedServices"
          @toggle="toggleServiceSelectMode"
        />
      </v-col>
    </v-row>
    <v-row>
      <v-col
        cols="12"
        sm="4"
        md="3"
        lg="2"
        v-for="(item, index) in <any[]>services"
        :key="item.tag"
        class="manual-drop-grid-cell"
        :class="serviceDrag.indicatorClasses(item.id)"
        :style="serviceDrag.indicatorStyles(item.id)"
        :draggable="false"
        @pointerdown="serviceDrag.prepare($event)"
        @dragstart="serviceDrag.start($event, item.id)"
        @dragover="serviceDrag.overTarget($event, item.id, services.map(row => row.id), serviceSelectMode ? selectedServiceIds.map(Number) : [], false, 'grid')"
        @dragleave="serviceDrag.leaveTarget($event, item.id)"
        @drop="onServiceDrop($event, item.id)"
        @dragend="serviceDrag.clear($event)"
      >
        <v-card
          rounded="xl"
          elevation="5"
          min-width="200"
          :title="item.tag"
          class="services__card"
          :class="{ 'services__card--selected': isServiceSelected(item.id) }"
        >
          <div v-if="serviceSelectMode" class="services__select manual-drag-no-drag">
            <v-checkbox-btn
              :model-value="isServiceSelected(item.id)"
              :aria-label="$t('table.selectRow')"
              density="compact"
              @update:model-value="toggleServiceSelection(item.id, Boolean($event))"
            />
          </div>
          <v-card-subtitle style="margin-top: -15px;">
            <v-row>
              <v-col>{{ item.type }}</v-col>
            </v-row>
          </v-card-subtitle>
          <v-card-text>
            <v-row v-if="item.type != 'oom-killer'">
              <v-col>{{ $t('in.addr') }}</v-col>
              <v-col>
                {{ item.listen }}
              </v-col>
            </v-row>
            <v-row v-if="item.type != 'oom-killer'">
              <v-col>{{ $t('in.port') }}</v-col>
              <v-col>
                {{ item.listen_port }}
              </v-col>
            </v-row>
            <v-row v-if="item.type != 'oom-killer'">
              <v-col>{{ $t('objects.tls') }}</v-col>
              <v-col>
                {{ item.tls_id > 0 ? $t('enable') : $t('disable') }}
              </v-col>
            </v-row>
            <v-row v-if="item.type == 'oom-killer'">
              <v-col>{{ $t('types.oom.memoryLimit') }}</v-col>
              <v-col>{{ item.memory_limit || '-' }}</v-col>
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
                  <v-btn color="error" variant="outlined" @click="delSrv(item.id)">{{ $t('yes') }}</v-btn>
                  <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
                </v-card-actions>
              </v-card>
            </v-overlay>
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
import { Srv } from '@/types/services'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ServiceVue from '@/layouts/modals/Service.vue'
import { useUiMode } from '@/uiMode/useUiMode'
import { defineAsyncComponent } from 'vue'
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

const ServicesNexusList = defineAsyncComponent(
  () => import('@/views/services/ServicesNexusList.vue'),
)
const ServiceDrawer = defineAsyncComponent(
  () => import('@/components/nexus/drawers/ServiceDrawer.vue'),
)

const EntityForm = computed(() => (mode.value === 'nexus' ? ServiceDrawer : ServiceVue))

const services = computed((): Srv[] => {
  return <Srv[]> Data().services
})
const serviceSelection = useBulkSelection(services, item => item.id)
const serviceSelectMode = serviceSelection.active
const selectedServiceIds = serviceSelection.selectedIds
const selectedServiceCount = serviceSelection.selectedCount
const isServiceSelected = serviceSelection.isSelected
const toggleServiceSelection = serviceSelection.toggle
const toggleServiceSelectMode = serviceSelection.toggleActive

const tsTags = computed((): any[] => {
  return Data().endpoints?.filter((o:any) => o.type == "tailscale")?.map((o:any) => o.tag)
})

const ssTags = computed((): any[] => {
  return Data().inbounds?.filter((o:any) => o.type == "shadowsocks" && !o.users)?.map((o:any) => o.tag)
})

const inTags = computed((): any[] => {
  return [...Data().inbounds?.map((o:any) => o.tag).filter(t => t != null), ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)]
})

const tlsConfigs = computed((): any[] => {
  return <any[]> Data().tlsConfigs
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(services.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const delSrv = async (id: number) => {
  const index = services.value.findIndex(i => i.id == id)
  const tag = services.value[index].tag

  const success = await Data().save("services", "del", tag)
  if (success) delOverlay.value[index] = false
}

const delServicesBulk = async (ids: number[]) => {
  const uniqueIds = [...new Set(ids.map(Number).filter(Boolean))]
  let success = true
  for (const id of uniqueIds) {
    const service = services.value.find(item => item.id === id)
    if (!service) continue
    success = await Data().save("services", "del", service.tag)
    if (!success) break
  }
  if (success) {
    delOverlay.value = []
    serviceSelection.clear()
  }
  return success
}

const deleteSelectedServices = async () => {
  const rows = serviceSelection.selectedItems.value
  if (rows.length === 0) return
  const accepted = await confirm({
    title: `${t('actions.delbulk')} ${t('objects.service')}`,
    message: rows.map(item => item.tag).join('\n'),
    confirmLabel: t('actions.del'),
    tone: 'error',
  })
  if (!accepted) return
  await delServicesBulk(rows.map(item => item.id))
}

const moveSrv = async (id: number, dir: number) => {
  await moveManualOrder("services", services.value as any[], id, dir)
}

const dragSrv = async (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
  await dragManualOrder("services", services.value as any[], draggedId, targetId, "id", position)
}

const dragSelectedServices = async (draggedIds: number[], targetId: number, position: ManualDropPosition | null = null) => {
  await moveManyManualOrder("services", services.value as any[], draggedIds, targetId, "id", position)
}

const sortServicesByName = async (direction: ManualSortDirection) => {
  await sortManualOrderByText("services", services.value as any[], direction, "tag")
}

const serviceDrag = useManualDrag<number>()
const onServiceDrop = (event: DragEvent, targetId: number) => {
  serviceDrag.drop(event, targetId, (draggedId, dropTargetId, position) => {
    if (serviceSelectMode.value && serviceSelection.isSelected(draggedId)) {
      void dragSelectedServices(serviceSelection.selectedIds.value.map(Number), dropTargetId, position)
      return
    }
    void dragSrv(draggedId, dropTargetId, position)
  })
}
</script>

<style scoped>
.services__card {
  position: relative;
}

.services__card--selected {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}

.services__select {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 2;
}
</style>
