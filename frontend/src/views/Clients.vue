
<template>
  <ClientModal 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeModal"
  />
  <ClientAddBulk 
    v-model="addBulkModal"
    :visible="addBulkModal"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeAddBulk"
  />
  <ClientEditBulk 
    v-model="editBulkModal"
    :visible="editBulkModal"
    :inboundTags="inboundTags"
    :clients="clients"
    @close="closeEditBulk"
  />
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :id="qrcode.id"
    @close="closeQrCode"
  />
  <ClientDoctor
    v-model="doctor.visible"
    :visible="doctor.visible"
    :id="doctor.id"
    @close="closeDoctor"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <IpHistoryModal
    v-model:visible="ipModal.visible"
    :client="ipModal.client"
    :is-admin="true"
    @cleared="onClientIpsCleared"
  />

  <ClientsNexusList
    v-if="mode === 'nexus'"
    :clients="<any[]>clients"
    :inbounds="<any[]>inbounds"
    :groups="groups"
    :onlines="onlineUsers"
    :enable-traffic="enableTraffic"
    @add="showModal(0)"
    @add-bulk="addBulk"
    @del="delClient"
    @del-many="delClientsBulk"
    @diagnose="showDoctor"
    @edit="showModal"
    @edit-bulk="editBulk"
    @qr="showQrCode"
    @move="moveClient"
    @move-many-to="dragSelectedClients"
    @move-to="dragClient"
    @sort-by-name="sortClientsByName"
    @show-ips="showClientIps"
    @stats="showStats"
  />

  <template v-else>
  <v-row justify="center" align="center">
    <v-col cols="auto">
      <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
    </v-col>
    <v-col cols="auto">
      <ManualSortButton
        :disabled="clients.length < 2"
        @sort="sortClientsByName"
      />
    </v-col>
    <v-col cols="auto">
      <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="text" icon>
            <v-icon icon="mdi-tools" color="primary" />
          </v-btn>
        </template>
        <v-list density="compact" nav>
          <v-list-item link @click="addBulk">
            <template v-slot:prepend>
              <v-icon icon="mdi-account-multiple-plus"></v-icon>
            </template>
            <v-list-item-title v-text="$t('actions.addbulk')"></v-list-item-title>
          </v-list-item>
          <v-list-item link @click="editBulk">
            <template v-slot:prepend>
              <v-icon icon="mdi-account-multiple-check"></v-icon>
            </template>
            <v-list-item-title v-text="$t('actions.editbulk')"></v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
    </v-col>
    <v-col cols="auto">
      <v-menu v-model="filterMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="text" icon>
            <v-icon :icon="filterSettings.enabled ? 'mdi-filter-check-outline' : 'mdi-filter-menu-outline'" :color="filterSettings.enabled ? 'primary' : ''" />
          </v-btn>
        </template>
        <v-card>
          <v-container>
            <v-row>
              <v-col>
                <v-select
                variant="underlined"
                density="compact"
                :label="$t('type')"
                :items="filterItems"
                v-model="filterSettings.state">
                </v-select>
              </v-col>
            </v-row>
            <v-row>
              <v-col>
                <v-select
                variant="underlined"
                density="compact"
                :label="$t('client.group')"
                :items="[ {title: $t('all'), value: '-'}, ...groups.map(g => ({ title: g.length>0 ? g : $t('none'), value: g}))]"
                v-model="filterSettings.group">
                </v-select>
              </v-col>
            </v-row>
            <v-row>
              <v-col>
                <v-text-field
                variant="underlined"
                density="compact"
                :label="$t('client.name')"
                v-model="filterSettings.text">
                </v-text-field>
              </v-col>
            </v-row>
          </v-container>
          <v-card-actions>
            <v-card-actions>
              <v-spacer></v-spacer>
              <v-btn
                color="blue-darken-1"
                variant="outlined"
                @click="clearFilter"
              >
                {{ $t('actions.del') }}
              </v-btn>
              <v-btn
                color="blue-darken-1"
                variant="tonal"
                @click="doFilter"
              >
                {{ $t('actions.update') }}
              </v-btn>
            </v-card-actions>
          </v-card-actions>
        </v-card>
      </v-menu>
    </v-col>
    <v-col cols="auto">
      <BulkSelectionControls
        :active="clientSelectMode"
        :count="selectedClientCount"
        :disabled="filterSettings.enabled"
        inactive-color="secondary"
        inactive-variant="outlined"
        @delete="deleteSelectedClients"
        @toggle="toggleClientSelectMode"
      />
    </v-col>
  </v-row>
  <v-row>
    <v-col cols="12">
      <v-data-table
        :headers="headers"
        :items="filterSettings.enabled ? filterSettings.filteredClients : clients"
        :hide-default-footer="filterSettings.enabled ? filterSettings.filteredClients.length<=10 : clients.length<=10"
        :items-per-page="itemPerPage"
        @update:items-per-page="setItemPerPage($event)"
        hide-no-data
        fixed-header
        item-value="id"
        :show-select="clientSelectMode"
        v-model="selectedClientIds"
        :mobile="smAndDown"
        mobile-breakpoint="sm"
        :row-props="clientRowProps"
        width="100%"
        class="elevation-3 rounded"
        >
        <template v-slot:item.inbounds="{ item }">
          <span>
          <v-tooltip activator="parent" dir="ltr" location="start" v-if="item.inbounds != ''">
            <span v-for="i in item.inbounds">{{ inbounds.find(inb => inb.id == i)?.tag }}<br /></span>
          </v-tooltip>
          {{ item.inbounds?.length }}
          </span>
        </template>
        <template v-slot:item.volume="{ item }">
          <div class="text-start" v-tooltip:top="'↓' + formatSize(item.down) + ' - ' + formatSize(item.up) + '↑'">
            <v-chip
              size="small"
              :color="item.volume==0 ? 'success' : item.volume<=(item.up + item.down)? 'error': ''"
              label
            >{{ formatSize(item.up + item.down) + ' / ' + (item.volume == 0 ? $t('unlimited') : formatSize(item.volume)) }}</v-chip>
          </div>
          <v-progress-linear
            :model-value="percent(item)"
            :color="percentColor(item)"
            v-if="item.volume>0"
            bottom
          >
          </v-progress-linear>
        </template>
        <template v-slot:item.expiry="{ item }">
          <div class="text-start">
            <v-tooltip v-if="item.expiry>0" activator="parent" location="top" :text="new Date(item.expiry * 1000).toLocaleString(locale)" />
            <v-chip
              size="small"
              :color="item.expiry==0 ? 'success' : item.expiry<=Date.now()/1000? 'error': ''"
              label
            >{{ remainedDays(item.expiry) }}</v-chip>
          </div>
        </template>
        <template v-slot:item.online="{ item }">
          <div class="text-start">
            <template v-if="isOnline(item.name).value">
              <v-chip density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
            </template>
            <template v-else>-</template>
          </div>
        </template>
        <template v-slot:item.lastIpCount="{ item }">
          <v-chip size="small" label @click="showClientIps(item.name)">
            {{ item.lastIpCount ?? 0 }}
          </v-chip>
        </template>
        <template v-slot:item.actions="{ item }">
        <v-icon
          class="me-2"
          @click="showModal(item.id)"
        >
          mdi-pencil
        </v-icon>
        <v-menu
          v-model="delOverlay[clients.findIndex(o => o.id == item.id)]"
          :close-on-content-click="false"
          location="top center"
        >
          <template v-slot:activator="{ props }">
            <v-icon
              class="me-2"
              color="error"
              v-bind="props"
            >
              mdi-delete
            </v-icon>
          </template>
          <v-card :title="$t('actions.del')" rounded="lg">
            <v-divider></v-divider>
            <v-card-text>{{ $t('confirm') }}</v-card-text>
            <v-card-actions>
              <v-btn color="error" variant="outlined" @click="delClient(item.id)">{{ $t('yes') }}</v-btn>
              <v-btn color="success" variant="outlined" @click="delOverlay[clients.findIndex(o => o.id == item.id)] = false">{{ $t('no') }}</v-btn>
            </v-card-actions>
          </v-card>
        </v-menu>
        <v-icon
          class="me-2"
          @click="showQrCode(item.id)"
        >
          mdi-qrcode
        </v-icon>
        <v-icon class="me-2" icon="lucide:activity" @click="showDoctor(item.id)">
          <v-tooltip activator="parent" location="top" :text="$t('actions.diagnose')"></v-tooltip>
        </v-icon>
        <v-icon icon="mdi-chart-line" @click="showStats(item.name)" v-if="enableTraffic">
          <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
        </v-icon>
      </template>
      </v-data-table>
    </v-col>
  </v-row>
  </template>
</template>
<style lang="scss" src="./Clients.scss"></style>
<script lang="ts" setup>
import ManualSortButton from '@/components/ManualSortButton.vue'
import BulkSelectionControls from '@/shared/ui/BulkSelectionControls.vue'
import ClientModal from '@/layouts/modals/Client.vue'
import ClientAddBulk from '@/layouts/modals/ClientAddBulk.vue'
import ClientEditBulk from '@/layouts/modals/ClientEditBulk.vue'
import QrCode from '@/layouts/modals/QrCode.vue'
import ClientDoctor from '@/layouts/modals/ClientDoctor.vue'
import Stats from '@/layouts/modals/Stats.vue'
import IpHistoryModal from '@/components/security/IpHistoryModal.vue'
import ClientsNexusList from '@/views/clients/ClientsNexusList.vue'
import { useClientsPage } from '@/shared/composables/pages/useClientsPage'

const { actionMenu, addBulk, addBulkModal, clearFilter, clientRowProps, clientSelectMode, clients, closeAddBulk, closeDoctor, closeEditBulk, closeModal, closeQrCode, closeStats, delClient, delClientsBulk, deleteSelectedClients, delOverlay, doFilter, doctor, dragClient, dragSelectedClients, editBulk, editBulkModal, enableTraffic, filterItems, filterMenu, filterSettings, formatSize, groups, headers, inboundTags, inbounds, ipModal, isOnline, itemPerPage, locale, modal, mode, moveClient, onClientIpsCleared, onlineUsers, percent, percentColor, qrcode, remainedDays, selectedClientCount, selectedClientIds, setItemPerPage, showClientIps, showDoctor, showModal, showQrCode, showStats, smAndDown, sortClientsByName, stats, toggleClientSelectMode } = useClientsPage()
</script>
