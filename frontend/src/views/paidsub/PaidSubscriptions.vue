<template>
  <page-header v-if="nexus" :title="$t('pages.paidSub')" />
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <template v-if="!nexus">
        <v-icon class="mr-2">mdi-cash-multiple</v-icon>
        {{ $t('pages.paidSub') }}
      </template>
      <v-chip :class="nexus ? '' : 'ml-3'" color="warning" size="small" variant="flat">{{ $t('paidSub.experimental') }}</v-chip>
      <v-spacer />
      <v-btn color="primary" :loading="loading" variant="tonal" @click="reloadAll">
        <v-icon start>lucide:rotate-cw</v-icon>{{ $t('actions.refresh') }}
      </v-btn>
    </v-card-title>

    <v-alert
      v-if="!secretboxKeySet"
      type="warning"
      variant="tonal"
      class="mx-4 mb-2"
      density="comfortable"
    >
      {{ $t('paidSub.secretboxWarning') }}
    </v-alert>

    <v-tabs v-model="tab" color="primary" class="px-2">
      <v-tab value="bindings">{{ $t('paidSub.tabs.bindings') }}</v-tab>
      <v-tab value="autoreg">{{ $t('paidSub.tabs.autoreg') }}</v-tab>
      <v-tab value="tariffs">{{ $t('paidSub.tabs.tariffs') }}</v-tab>
      <v-tab value="payments">{{ $t('paidSub.tabs.payments') }}</v-tab>
      <v-tab value="messages">{{ $t('paidSub.tabs.messages') }}</v-tab>
      <v-tab value="orders">{{ $t('paidSub.tabs.orders') }}</v-tab>
      <v-tab value="bot">{{ $t('paidSub.tabs.bot') }}</v-tab>
    </v-tabs>

    <v-window v-model="tab" class="pa-4">
      <!-- BINDINGS -->
      <PaidSubscriptionManagementTabs :page="page" />
      <PaidSubscriptionOrdersTabs :page="page" />
    </v-window>
  </v-card>

  <!-- Binding dialog -->
  <v-dialog v-model="bindingDialog" max-width="420">
    <v-card>
      <v-card-title>{{ bindingEdit.isNew ? $t('paidSub.bindingDialog.addTitle') : $t('paidSub.bindingDialog.editTitle', { name: bindingEdit.name }) }}</v-card-title>
      <v-card-text>
        <v-select
          v-if="bindingEdit.isNew"
          v-model="bindingEdit.clientId"
          :items="clientOptions"
          item-title="title"
          item-value="value"
          :label="$t('paidSub.bindingDialog.client')"
        />
        <v-text-field v-model="bindingEdit.tgUserId" type="number" :label="$t('paidSub.bindingDialog.tgId')" autofocus />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="bindingDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="primary" @click="saveBinding">{{ $t('actions.set') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Tariff dialog -->
  <v-dialog v-model="tariffDialog" max-width="560">
    <v-card>
      <v-card-title>{{ tariffEdit.id ? $t('paidSub.tariffs.edit') : $t('paidSub.tariffs.new') }}</v-card-title>
      <v-card-text>
        <v-text-field v-model="tariffEdit.name" :label="$t('paidSub.cols.name')" />
        <v-text-field v-model="tariffEdit.description" :label="$t('paidSub.cols.description')" />
        <v-row>
          <v-col cols="6"><v-text-field v-model.number="tariffEdit.priceMajor" type="number" :label="$t('paidSub.tariffs.priceMajor')" /></v-col>
          <v-col cols="6">
            <v-combobox
              v-model="tariffEdit.currency"
              :items="currencyOptions"
              :label="$t('paidSub.tariffs.currency')"
              maxlength="3"
              @update:model-value="tariffEdit.currency = normalizeCurrency($event)"
            />
          </v-col>
          <v-col cols="6"><v-text-field v-model.number="tariffEdit.starsAmount" type="number" :label="$t('paidSub.tariffs.starsAmount')" /></v-col>
          <v-col cols="6"><v-text-field v-model.number="tariffEdit.addDays" type="number" :label="$t('paidSub.tariffs.addDays')" /></v-col>
          <v-col cols="6"><v-text-field v-model.number="tariffEdit.addTrafficGB" type="number" :label="$t('paidSub.tariffs.addTrafficGB')" /></v-col>
          <v-col cols="6"><v-text-field v-model.number="tariffEdit.sort" type="number" :label="$t('paidSub.tariffs.sort')" /></v-col>
        </v-row>
        <v-switch v-model="tariffEdit.enabled" color="primary" :label="$t('paidSub.tariffs.enabledField')" hide-details />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="tariffDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="primary" @click="saveTariff">{{ $t('actions.set') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Broadcast confirm dialog -->
  <v-dialog v-model="broadcastDialog" max-width="460">
    <v-card>
      <v-card-title>{{ $t('paidSub.messages.confirmTitle') }}</v-card-title>
      <v-card-text>{{ $t('paidSub.messages.confirmText', { count: recipientCount }) }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="broadcastDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="primary" @click="sendBroadcast">{{ $t('paidSub.messages.send') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Refund confirm dialog -->
  <v-dialog v-model="refundDialog" max-width="480">
    <v-card>
      <v-card-title>{{ $t('paidSub.refund.title', { id: refundEdit.id }) }}</v-card-title>
      <v-card-text>
        <div class="mb-2">{{ refundEdit.provider }} · {{ formatMoney(refundEdit.amount, refundEdit.currency) }}</div>
        <v-alert :type="refundEdit.provider === 'stars' ? 'info' : 'warning'" variant="tonal" density="comfortable" class="mb-3">
          {{ refundEdit.provider === 'stars' ? $t('paidSub.refund.starsNote') : $t('paidSub.refund.manualNote') }}
        </v-alert>
        <v-switch
          v-model="refundEdit.revoke"
          color="primary"
          hide-details
          :label="$t('paidSub.refund.revoke')"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="refundDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="warning" :loading="refundBusy" @click="doRefund">{{ $t('paidSub.orders.refund') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Unbind confirm dialog -->
  <v-dialog v-model="unbindDialog" max-width="440">
    <v-card>
      <v-card-title>{{ $t('paidSub.unbind.title', { name: unbindEdit.name }) }}</v-card-title>
      <v-card-text>
        {{ $t('paidSub.unbind.text', { clientId: unbindEdit.clientId, tgUserId: unbindEdit.tgUserId }) }}
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="unbindDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="error" :loading="unbindBusy" @click="doUnbind">{{ $t('paidSub.unbind.confirm') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import SettingsSecretField from '@/components/settings/SettingsSecretField.vue'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import PaidSubscriptionManagementTabs from '@/components/subscription/PaidSubscriptionManagementTabs.vue'
import PaidSubscriptionOrdersTabs from '@/components/subscription/PaidSubscriptionOrdersTabs.vue'
import { usePaidSubscriptionsPage } from '@/shared/composables/pages/usePaidSubscriptionsPage'

const page = usePaidSubscriptionsPage()
const { bindingDialog, bindingEdit, bindings, broadcastDialog, clientOptions, currencyOptions, doRefund, doUnbind, enabled, formatMoney, loading, nexus, normalizeCurrency, orders, recipientCount, refundBusy, refundDialog, refundEdit, reloadAll, saveBinding, saveTariff, secretboxKeySet, sendBroadcast, settings, tab, tariffDialog, tariffEdit, tariffs, unbindBusy, unbindDialog, unbindEdit } = page
</script>

<style scoped lang="scss" src="./PaidSubscriptions.scss"></style>
