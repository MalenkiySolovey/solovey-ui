<template>
  <div class="remote-outbounds" :class="{ 'remote-outbounds--nexus': page.mode === 'nexus' }">
    <page-header
      v-if="page.mode === 'nexus'"
      v-model:search="page.search"
      searchable
      :subtitle="page.subtitle"
      :title="$t('pages.remoteOutboundSubscriptions')"
    />

    <page-toolbar v-if="page.mode === 'nexus'">
      <template #actions>
        <v-btn
          append-icon="lucide:gauge"
          :disabled="page.testingAll || page.totalTestableConnections === 0"
          :loading="page.testingAll"
          variant="outlined"
          @click="page.testAll"
        >
          {{ $t('actions.testAll') }}
        </v-btn>
        <v-btn prepend-icon="lucide:refresh-cw" :loading="page.loading" variant="tonal" @click="page.load">
          {{ $t('actions.refresh') }}
        </v-btn>
        <v-btn prepend-icon="mdi-cog" variant="tonal" @click="page.openConversionPolicy">
          {{ $t('remoteOutbound.conversionSettings') }}
        </v-btn>
        <v-btn
          :disabled="!page.canAddBulkGroup"
          :loading="page.savingBulkGroup"
          prepend-icon="lucide:plus"
          variant="tonal"
          @click="page.openBulkGroupDialog"
        >
          {{ $t('actions.addbulk') }}
        </v-btn>
      </template>
    </page-toolbar>

    <v-row v-else justify="center" align="center">
      <v-col cols="12" sm="6" md="4">
        <v-text-field
          v-model="page.search"
          density="compact"
          hide-details
          prepend-inner-icon="mdi-magnify"
          :label="$t('table.search')"
          variant="outlined"
        />
      </v-col>
      <v-col cols="auto">
        <v-btn color="secondary" variant="outlined" :loading="page.loading" @click="page.load">
          {{ $t('actions.refresh') }}
        </v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn color="secondary" prepend-icon="mdi-cog" variant="outlined" @click="page.openConversionPolicy">
          {{ $t('remoteOutbound.conversionSettings') }}
        </v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn
          color="secondary"
          prepend-icon="lucide:plus"
          variant="outlined"
          :disabled="!page.canAddBulkGroup"
          :loading="page.savingBulkGroup"
          @click="page.openBulkGroupDialog"
        >
          {{ $t('actions.addbulk') }}
        </v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn
          append-icon="mdi-speedometer"
          color="secondary"
          variant="outlined"
          :disabled="page.testingAll || page.totalTestableConnections === 0"
          :loading="page.testingAll"
          @click="page.testAll"
        >
          {{ $t('actions.testAll') }}
        </v-btn>
      </v-col>
    </v-row>

    <v-form ref="subscriptionForm" class="remote-outbounds__form" @submit.prevent="page.saveSubscription">
      <v-row align="center">
        <v-col cols="12" md="3">
          <v-text-field
            v-model="page.form.name"
            density="compact"
            :label="$t('remoteOutbound.name')"
            :rules="page.requiredRules"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="5">
          <v-text-field
            v-model="page.form.url"
            density="compact"
            :label="$t('remoteOutbound.url')"
            :rules="page.requiredRules"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="2">
          <v-text-field v-model="page.form.tagPrefix" density="compact" :label="$t('remoteOutbound.tagPrefix')" variant="outlined" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-switch v-model="page.form.enabled" color="primary" density="compact" hide-details :label="$t('enable')" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-switch v-model="page.form.autoUpdate" color="primary" density="compact" hide-details :label="$t('remoteOutbound.autoUpdate')" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-text-field
            v-model.number="page.updateIntervalMinutes"
            density="compact"
            hide-details
            min="5"
            :disabled="!page.form.autoUpdate"
            :label="$t('remoteOutbound.updateInterval')"
            suffix="min"
            type="number"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" sm="6" md="12" class="remote-outbounds__form-actions">
          <v-btn color="primary" :loading="page.saving" type="submit">
            {{ page.form.id ? $t('actions.update') : $t('actions.add') }}
          </v-btn>
          <v-btn v-if="page.form.id" variant="text" @click="page.resetForm">
            {{ $t('actions.cancel') }}
          </v-btn>
        </v-col>
      </v-row>
    </v-form>

    <v-progress-linear v-if="page.loading && page.subscriptions.length === 0" indeterminate />

    <RemoteOutboundSubscriptionPanels :page="page" />
    <RemoteOutboundSubscriptionDialogs :page="page" />
  </div>
</template>

<script lang="ts" setup>
import { reactive } from 'vue'
import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import RemoteOutboundSubscriptionDialogs from '@/components/remote/RemoteOutboundSubscriptionDialogs.vue'
import RemoteOutboundSubscriptionPanels from '@/components/remote/RemoteOutboundSubscriptionPanels.vue'
import { useRemoteOutboundSubscriptionsPage } from '@/shared/composables/pages/useRemoteOutboundSubscriptionsPage'

const rawPage = useRemoteOutboundSubscriptionsPage()
const page = reactive(rawPage)
const subscriptionForm = rawPage.subscriptionForm
</script>

<style scoped lang="scss" src="./RemoteOutboundSubscriptions.scss"></style>
