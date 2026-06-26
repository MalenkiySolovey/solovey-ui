<template>
      <v-window-item value="bindings">
        <div class="d-flex align-center mb-2">
          <div class="text-caption text-medium-emphasis">
            {{ $t('paidSub.bindings.hint') }}
          </div>
          <v-spacer />
          <v-btn color="primary" :disabled="bindings.length === 0" @click="openAddBinding()">
            <v-icon start>lucide:plus</v-icon>{{ $t('paidSub.bindings.add') }}
          </v-btn>
        </div>
        <v-alert v-if="!bindingsLoading && bindings.length === 0" type="info" variant="tonal" density="comfortable">
          {{ $t('paidSub.bindings.empty') }}
        </v-alert>
        <nexus-data-table
          v-else-if="nexus"
          :columns="bindingColumns"
          :items="bindings"
          :loading="bindingsLoading"
          :row-key="(item) => item.clientId"
        >
          <template #col.name="{ item }">
            <span class="paidsub-nexus__name">{{ item.name }}</span>
          </template>
          <template #col.enable="{ item }">
            <status-badge :label="item.enable ? $t('paidSub.active') : $t('paidSub.disabled')" :tone="item.enable ? 'success' : 'error'" />
          </template>
          <template #col.tgUserId="{ item }">
            <span v-if="item.tgUserId">{{ item.tgUserId }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #col.desc="{ item }">
            <span v-if="item.desc">{{ item.desc }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #col.expiry="{ item }">
            <template v-if="item.expiry > 0">
              {{ new Date(item.expiry * 1000).toLocaleString() }}
              (<v-chip size="x-small" label :color="item.expiry <= Date.now() / 1000 ? 'error' : ''">{{ remainedDays(item.expiry) }}</v-chip>)
            </template>
            <v-chip v-else size="small" color="success" label>{{ remainedDays(item.expiry) }}</v-chip>
          </template>
          <template #actions="{ item }">
            <row-actions :actions="bindingActions(item)" @action="(key) => handleBindingAction(key, item)" />
          </template>
          <template #empty>
            <empty-state icon="lucide:link" :title="$t('paidSub.bindings.none')" />
          </template>
        </nexus-data-table>
        <v-data-table v-else :headers="bindingHeaders" :items="bindings" :loading="bindingsLoading" density="comfortable">
          <template #item.enable="{ item }">
            <v-chip :color="item.enable ? 'success' : 'error'" size="small" variant="flat">
              {{ item.enable ? $t('paidSub.active') : $t('paidSub.disabled') }}
            </v-chip>
          </template>
          <template #item.tgUserId="{ item }">
            <span v-if="item.tgUserId">{{ item.tgUserId }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #item.desc="{ item }">
            <span v-if="item.desc">{{ item.desc }}</span>
            <span v-else class="text-disabled">—</span>
          </template>
          <template #item.expiry="{ item }">
            <template v-if="item.expiry > 0">
              {{ new Date(item.expiry * 1000).toLocaleString() }}
              (<v-chip size="x-small" label :color="item.expiry <= Date.now() / 1000 ? 'error' : ''">{{ remainedDays(item.expiry) }}</v-chip>)
            </template>
            <v-chip v-else size="small" color="success" label>{{ remainedDays(item.expiry) }}</v-chip>
          </template>
          <template #item.actions="{ item }">
            <v-btn size="small" variant="text" icon="mdi-pencil" @click="openBinding(item)" />
            <v-btn v-if="item.tgUserId" size="small" variant="text" icon="mdi-link-off" color="error" @click="openUnbindConfirm(item)" />
          </template>
        </v-data-table>
      </v-window-item>

      <!-- AUTO-REGISTRATION -->
      <v-window-item value="autoreg">
        <v-row>
          <v-col cols="12" md="6">
            <v-switch v-model="autoRegister" color="primary" :label="$t('paidSub.autoreg.enable')" hide-details />
          </v-col>
          <v-col cols="12" md="6">
            <v-select
              v-model="autoInbounds"
              :items="inboundOptions"
              item-title="title"
              item-value="value"
              :label="$t('paidSub.autoreg.inbounds')"
              multiple
              chips
            />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="settings.paidSubTrialDays" type="number" :label="$t('paidSub.autoreg.trialDays')" />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="settings.paidSubTrialVolumeGB" type="number" :label="$t('paidSub.autoreg.trialVolume')" />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="settings.paidSubMaxClients" type="number" :label="$t('paidSub.autoreg.maxClients')" />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="settings.paidSubStartRateLimitPerMin" type="number" :label="$t('paidSub.autoreg.rateLimit')" />
          </v-col>
        </v-row>
        <v-btn color="primary" :loading="loading" @click="saveSettings">{{ $t('actions.set') }}</v-btn>
      </v-window-item>

      <!-- TARIFFS -->
      <v-window-item value="tariffs">
        <div class="d-flex mb-2">
          <v-spacer />
          <v-btn color="primary" @click="openTariff()"><v-icon start>lucide:plus</v-icon>{{ $t('paidSub.tariffs.add') }}</v-btn>
        </div>
        <nexus-data-table
          v-if="nexus"
          :columns="tariffColumns"
          :items="tariffs"
          :loading="tariffsLoading"
          :row-key="(item) => item.id"
        >
          <template #col.name="{ item }">
            <span class="paidsub-nexus__name">{{ item.name }}</span>
          </template>
          <template #col.price="{ item }">{{ (item.price / 100).toFixed(2) }} {{ item.currency }}</template>
          <template #col.starsAmount="{ item }">{{ item.starsAmount || '—' }}</template>
          <template #col.addTrafficBytes="{ item }">{{ item.addTrafficBytes ? (item.addTrafficBytes / (1024*1024*1024)).toFixed(2) + ' GB' : '∞' }}</template>
          <template #col.enabled="{ item }">
            <status-badge :label="item.enabled ? $t('nexus.on') : $t('nexus.off')" :tone="item.enabled ? 'success' : 'info'" />
          </template>
          <template #actions="{ item }">
            <row-actions :actions="tariffActions()" @action="(key) => handleTariffAction(key, item)" />
          </template>
          <template #empty>
            <empty-state icon="lucide:tag" :title="$t('paidSub.tariffs.none')" />
          </template>
        </nexus-data-table>
        <v-data-table v-else :headers="tariffHeaders" :items="tariffs" :loading="tariffsLoading" density="comfortable">
          <template #item.price="{ item }">{{ (item.price / 100).toFixed(2) }} {{ item.currency }}</template>
          <template #item.starsAmount="{ item }">{{ item.starsAmount || '—' }}</template>
          <template #item.addTrafficBytes="{ item }">{{ item.addTrafficBytes ? (item.addTrafficBytes / (1024*1024*1024)).toFixed(2) + ' GB' : '∞' }}</template>
          <template #item.enabled="{ item }">
            <v-chip :color="item.enabled ? 'success' : 'grey'" size="small" variant="flat">{{ item.enabled ? $t('nexus.on') : $t('nexus.off') }}</v-chip>
          </template>
          <template #item.actions="{ item }">
            <v-btn size="small" variant="text" icon="mdi-pencil" @click="openTariff(item)" />
            <v-btn size="small" variant="text" icon="mdi-delete" color="error" @click="deleteTariff(item)" />
          </template>
        </v-data-table>
      </v-window-item>

      <!-- PAYMENTS -->
      <v-window-item value="payments">
        <v-row>
          <v-col cols="12" md="4">
            <v-combobox
              v-model="settings.paidSubCurrency"
              :items="currencyOptions"
              :label="$t('paidSub.payments.currency')"
              maxlength="3"
              @update:model-value="settings.paidSubCurrency = normalizeCurrency($event)"
            />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="settings.paidSubOrderTTLMinutes" type="number" :label="$t('paidSub.payments.orderTtl')" />
          </v-col>
        </v-row>
        <v-divider class="my-2" />
        <v-switch v-model="starsEnabled" color="primary" :label="$t('paidSub.payments.stars')" hide-details />
        <v-divider class="my-2" />
        <v-switch v-model="yooEnabled" color="primary" :label="$t('paidSub.payments.yookassa')" hide-details />
        <SettingsSecretField
          v-model="settings.paidSubYooKassaToken"
          :has-secret="settings.paidSubYooKassaTokenHasSecret"
          :label="$t('paidSub.payments.yookassaToken')"
        />
        <v-divider class="my-2" />
        <v-switch v-model="stripeEnabled" color="primary" :label="$t('paidSub.payments.stripe')" hide-details />
        <SettingsSecretField
          v-model="settings.paidSubStripeToken"
          :has-secret="settings.paidSubStripeTokenHasSecret"
          :label="$t('paidSub.payments.stripeToken')"
        />
        <v-divider class="my-2" />
        <v-switch v-model="paymasterEnabled" color="primary" :label="$t('paidSub.payments.paymaster')" hide-details />
        <SettingsSecretField
          v-model="settings.paidSubPayMasterToken"
          :has-secret="settings.paidSubPayMasterTokenHasSecret"
          :label="$t('paidSub.payments.paymasterToken')"
        />
        <v-divider class="my-2" />
        <v-switch v-model="cryptoEnabled" color="primary" :label="$t('paidSub.payments.crypto')" hide-details />
        <SettingsSecretField
          v-model="settings.paidSubCryptoBotToken"
          :has-secret="settings.paidSubCryptoBotTokenHasSecret"
          :label="$t('paidSub.payments.cryptoToken')"
        />
        <v-divider class="my-2" />
        <v-switch v-model="externalEnabled" color="primary" :label="$t('paidSub.payments.external')" hide-details />
        <v-text-field
          v-model="settings.paidSubExternalUrlTemplate"
          :label="$t('paidSub.payments.externalTemplate')"
        />
        <v-btn class="mt-2" color="primary" :loading="loading" @click="saveSettings">{{ $t('actions.set') }}</v-btn>
      </v-window-item>

      <!-- MESSAGES -->
      <v-window-item value="messages">
        <div class="text-subtitle-2 mb-1">{{ $t('paidSub.messages.greetingTitle') }}</div>
        <div class="text-caption text-medium-emphasis mb-2">
          {{ $t('paidSub.messages.greetingHint') }}
        </div>
        <v-textarea v-model="settings.paidSubGreeting" :label="$t('paidSub.messages.greetingLabel')" rows="3" auto-grow counter="4096" />
        <v-btn color="primary" :loading="loading" @click="saveSettings">{{ $t('actions.set') }}</v-btn>

        <v-divider class="my-4" />

        <div class="text-subtitle-2 mb-1">{{ $t('paidSub.messages.broadcastTitle') }}</div>
        <div class="text-caption text-medium-emphasis mb-2">
          {{ $t('paidSub.messages.broadcastHint', { count: recipientCount }) }}
        </div>
        <v-textarea v-model="broadcastText" :label="$t('paidSub.messages.broadcastLabel')" rows="4" auto-grow counter="4096" />
        <v-btn color="primary" :loading="broadcastLoading" :disabled="!broadcastText.trim() || recipientCount === 0" @click="broadcastDialog = true">
          <v-icon start>lucide:megaphone</v-icon>{{ $t('paidSub.messages.sendAll') }}
        </v-btn>
        <v-alert v-if="broadcastResult" type="info" variant="tonal" class="mt-3" density="comfortable">
          {{ $t('paidSub.messages.result', { sent: broadcastResult.sent, failed: broadcastResult.failed }) }}
        </v-alert>
      </v-window-item>

      <!-- ORDERS -->
</template>

<script lang="ts" setup>
import SettingsSecretField from '@/components/settings/SettingsSecretField.vue'
import NexusDataTable from '@/components/nexus/data/NexusDataTable.vue'
import RowActions from '@/components/nexus/data/RowActions.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import StatusBadge from '@/components/nexus/primitives/StatusBadge.vue'
import type { PaidSubscriptionsPage } from '@/shared/composables/pages/usePaidSubscriptionsPage'

const props = defineProps<{ page: PaidSubscriptionsPage }>()
const { autoInbounds, autoRegister, bindingActions, bindingColumns, bindingHeaders, bindings, bindingsLoading, broadcastDialog, broadcastLoading, broadcastResult, broadcastText, cryptoEnabled, currencyOptions, deleteTariff, enabled, externalEnabled, handleBindingAction, handleTariffAction, inboundOptions, loading, nexus, normalizeCurrency, openAddBinding, openBinding, openTariff, openUnbindConfirm, paymasterEnabled, recipientCount, remainedDays, saveSettings, settings, starsEnabled, stripeEnabled, tariffActions, tariffColumns, tariffHeaders, tariffs, tariffsLoading, yooEnabled } = props.page

</script>
