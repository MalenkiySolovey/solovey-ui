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
    :failover-status="failoverStatus"
    :order-dirty="outboundOrderDirty"
    :order-saving="outboundOrderSaving"
    :testing-all="testingAll"
    @add="showModal(0)"
    @add-bulk="showBulkModal"
    @cancel-order="cancelOutboundOrder"
    @del="delOutbound"
    @del-many="delOutboundsBulk"
    @edit="showModal"
    @move="moveOutbound"
    @move-many-to="dragSelectedOutbounds"
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
        <BulkSelectionControls
          :active="outboundSelectMode"
          :count="selectedOutboundCount"
          inactive-color="secondary"
          inactive-variant="outlined"
          @delete="deleteSelectedOutbounds"
          @toggle="toggleOutboundSelectMode"
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
        class="manual-drop-grid-cell"
        :class="outboundDrag.indicatorClasses(item.id)"
        :style="outboundDrag.indicatorStyles(item.id)"
        :draggable="false"
        @pointerdown="outboundDrag.prepare($event)"
        @dragstart="outboundDrag.start($event, item.id)"
        @dragover="outboundDrag.overTarget($event, item.id, orderedOutbounds.map(row => row.id), outboundSelectMode ? selectedOutboundIds : [], false, 'grid')"
        @dragleave="outboundDrag.leaveTarget($event, item.id)"
        @drop="onOutboundDrop($event, item.id)"
        @dragend="outboundDrag.clear($event)"
      >
        <v-card
          rounded="xl"
          elevation="5"
          min-width="200"
          :title="item.tag"
          class="outbounds__card"
          :class="{ 'outbounds__card--selected': isOutboundSelected(item.id) }"
        >
          <div v-if="outboundSelectMode" class="outbounds__select manual-drag-no-drag">
            <v-checkbox-btn
              :model-value="isOutboundSelected(item.id)"
              :aria-label="$t('table.selectRow')"
              density="compact"
              @update:model-value="toggleOutboundSelection(item.id, Boolean($event))"
            />
          </div>
          <v-card-subtitle style="margin-top: -15px;">
            <v-row>
              <v-col>{{ item.type }}</v-col>
              <v-col v-if="item.remoteOutboundManaged">
                <v-chip color="info" density="compact" size="small" variant="tonal">
                  {{ $t('remoteOutbound.managedOutbound') }}
                </v-chip>
                <v-chip v-if="item.remoteMissing" class="ml-1" color="warning" density="compact" size="small" variant="flat">
                  {{ $t('remoteOutbound.missing') }}
                </v-chip>
              </v-col>
            </v-row>
            <div v-if="item.remoteMissing" class="outbounds__remote-missing">
              {{ item.remoteMissingSource || item.remoteOutboundSubscription || item.remoteOutboundConnection || item.remoteMissingReason }}
            </div>
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
            <v-btn icon="mdi-chart-line" @click="showStats(item.tag)" v-if="enableTraffic">
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
import ManualOrderControls from '@/shared/ui/ManualOrderControls.vue'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import OutboundVue from '@/layouts/modals/Outbound.vue'
import OutboundBulk from '@/layouts/modals/OutboundBulk.vue'
import Stats from '@/layouts/modals/Stats.vue'
import { useOutboundsPage } from '@/shared/composables/pages/useOutboundsPage'

const { mode, EntityForm, OutboundsNexusList, checkResults, testingAll, checkOutbound, checkAllOutbounds, failoverStatus, outbounds, orderedOutbounds, outboundOrderDirty, outboundOrderSaving, outboundTags, onlines, enableTraffic, modal, showModal, closeModal, bulkModal, showBulkModal, closeBulkModal, stats, showStats, closeStats, delOverlay, delOutbound, delOutboundsBulk, deleteSelectedOutbounds, outboundSelectMode, selectedOutboundCount, selectedOutboundIds, isOutboundSelected, toggleOutboundSelectMode, toggleOutboundSelection, moveOutbound, dragOutbound, dragSelectedOutbounds, sortOutboundsByName, saveOutboundOrder, cancelOutboundOrder, outboundDrag, onOutboundDrop } = useOutboundsPage(OutboundVue)
</script>

<style scoped>
.outbounds__card {
  position: relative;
}

.outbounds__card--selected {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}

.outbounds__select {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 2;
}

.outbounds__delete-hint {
  color: rgb(var(--v-theme-warning));
  font-size: 0.82rem;
  line-height: 1.35;
  margin-top: 8px;
  max-width: 260px;
}

.outbounds__remote-missing {
  color: rgb(var(--v-theme-warning));
  font-size: 0.74rem;
  line-height: 1.25;
  margin-top: 4px;
}
</style>
