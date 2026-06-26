<template>
      <v-window-item value="orders">
        <nexus-data-table
          v-if="nexus"
          :columns="orderColumns"
          :items="orders"
          :loading="ordersLoading"
          :row-key="(item) => item.id"
        >
          <template #col.clientName="{ item }">
            <span v-if="item.clientName">{{ item.clientName }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #col.telegramUserId="{ item }">
            <span v-if="item.telegramUserId">{{ item.telegramUserId }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #col.clientDesc="{ item }">
            <span v-if="item.clientDesc">{{ item.clientDesc }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #col.amount="{ item }">{{ formatMoney(item.amount, item.currency) }}</template>
          <template #col.status="{ item }">
            <status-badge :label="item.status" :tone="orderStatusTone(item.status)" />
          </template>
          <template #col.createdAt="{ item }">{{ item.createdAt ? new Date(item.createdAt * 1000).toLocaleString() : '' }}</template>
          <template #actions="{ item }">
            <row-actions :actions="orderActions(item)" @action="(key) => handleOrderAction(key, item)" />
          </template>
          <template #empty>
            <empty-state icon="lucide:receipt" :title="$t('table.noData')" />
          </template>
        </nexus-data-table>
        <v-data-table v-else :headers="orderHeaders" :items="orders" :loading="ordersLoading" density="comfortable">
          <template #item.clientName="{ item }">
            <span v-if="item.clientName">{{ item.clientName }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #item.telegramUserId="{ item }">
            <span v-if="item.telegramUserId">{{ item.telegramUserId }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #item.clientDesc="{ item }">
            <span v-if="item.clientDesc">{{ item.clientDesc }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #item.amount="{ item }">{{ formatMoney(item.amount, item.currency) }}</template>
          <template #item.status="{ item }">
            <v-chip :color="orderStatusColor(item.status)" size="small" variant="flat">{{ item.status }}</v-chip>
          </template>
          <template #item.createdAt="{ item }">{{ item.createdAt ? new Date(item.createdAt * 1000).toLocaleString() : '' }}</template>
          <template #item.actions="{ item }">
            <v-btn v-if="item.status === 'paid'" size="small" variant="text" color="warning" @click="openRefund(item)">{{ $t('paidSub.orders.refund') }}</v-btn>
          </template>
        </v-data-table>
      </v-window-item>

      <!-- BOT -->
      <v-window-item value="bot">
        <v-row>
          <v-col cols="12" md="6">
            <v-switch v-model="enabled" color="primary" :label="$t('paidSub.bot.enable')" hide-details />
          </v-col>
          <v-col cols="12" md="6">
            <v-text-field v-model="settings.paidSubBotPollSeconds" type="number" :label="$t('paidSub.bot.pollTimeout')" />
          </v-col>
          <v-col cols="12">
            <SettingsSecretField
              v-model="settings.paidSubBotToken"
              :has-secret="settings.paidSubBotTokenHasSecret"
              :label="$t('paidSub.bot.token')"
            />
          </v-col>
        </v-row>

        <v-divider class="my-3" />
        <div class="text-subtitle-2 mb-1">{{ $t('paidSub.bot.transportTitle') }}</div>
        <div class="text-caption text-medium-emphasis mb-2">
          {{ $t('paidSub.bot.transportHint') }}
        </div>
        <v-row>
          <v-col cols="12" md="4">
            <v-select
              v-model="settings.paidSubTransportMode"
              :items="transportModes"
              item-title="title"
              item-value="value"
              :label="$t('paidSub.bot.transport')"
            />
          </v-col>
          <v-col v-if="settings.paidSubTransportMode === 'outbound'" cols="12" md="8">
            <v-select
              v-model="settings.paidSubOutboundTag"
              :items="outboundOptions"
              item-title="title"
              item-value="value"
              :label="$t('paidSub.bot.outbound')"
              :hint="outboundOptions.length === 0 ? $t('paidSub.bot.noOutbounds') : ''"
              persistent-hint
            />
          </v-col>
          <template v-else>
            <v-col cols="12" md="8">
              <SettingsSecretField
                v-model="settings.paidSubProxyURL"
                :has-secret="settings.paidSubProxyURLHasSecret"
                :label="$t('paidSub.bot.proxyUrl')"
              />
            </v-col>
            <v-col cols="12" md="6">
              <SettingsSecretField
                v-model="settings.paidSubProxyUsername"
                :has-secret="settings.paidSubProxyUsernameHasSecret"
                :label="$t('paidSub.bot.proxyUser')"
              />
            </v-col>
            <v-col cols="12" md="6">
              <SettingsSecretField
                v-model="settings.paidSubProxyPassword"
                :has-secret="settings.paidSubProxyPasswordHasSecret"
                :label="$t('paidSub.bot.proxyPass')"
              />
            </v-col>
          </template>
        </v-row>

        <v-btn color="primary" :loading="loading" @click="saveSettings">{{ $t('actions.set') }}</v-btn>
      </v-window-item>
</template>

<script lang="ts" setup>
import SettingsSecretField from '@/components/settings/SettingsSecretField.vue'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import type { PaidSubscriptionsPage } from '@/shared/composables/pages/usePaidSubscriptionsPage'

const props = defineProps<{ page: PaidSubscriptionsPage }>()
const { enabled, formatMoney, handleOrderAction, loading, nexus, openRefund, orderActions, orderColumns, orderHeaders, orderStatusColor, orderStatusTone, orders, ordersLoading, outboundOptions, saveSettings, settings, transportModes } = props.page

</script>
