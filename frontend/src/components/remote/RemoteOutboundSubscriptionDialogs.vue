<template>
  <v-dialog v-model="page.bulkGroupDialog" max-width="460">
    <v-card>
      <v-card-title class="remote-outbounds__dialog-heading">
        <v-icon icon="lucide:plus" size="20" />
        <span>{{ $t('actions.addbulk') }}: {{ $t('remoteOutbound.group') }}</span>
      </v-card-title>
      <v-card-text>
        <v-text-field
          v-model="page.bulkGroupName"
          autofocus
          density="compact"
          :label="$t('remoteOutbound.newGroup')"
          variant="outlined"
          @keyup.enter="page.saveBulkGroup"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="page.bulkGroupDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="primary" :disabled="!page.bulkGroupName.trim()" :loading="page.savingBulkGroup" @click="page.saveBulkGroup">
          {{ $t('actions.add') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="page.conversionDialog" max-width="1100">
    <v-card>
      <v-card-title class="remote-outbounds__dialog-heading">
        <v-icon icon="mdi-cog" size="20" />
        <span>{{ $t('remoteOutbound.conversionSettings') }}</span>
      </v-card-title>
      <v-card-subtitle>{{ $t('remoteOutbound.conversionHint') }}</v-card-subtitle>
      <v-card-text>
        <h3 class="remote-outbounds__dialog-title">
          <v-icon icon="mdi-sync" size="18" />
          <span>{{ $t('remoteOutbound.outboundConversion') }}</span>
        </h3>
        <v-table density="compact" class="remote-outbounds__policy-table">
          <thead>
            <tr>
              <th>{{ $t('remoteOutbound.sourceFeature') }}</th>
              <th>{{ $t('remoteOutbound.outbound') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="feature in page.conversionFeatures" :key="`outbound-${feature.key}`">
              <td>{{ feature.label }}</td>
              <td>
                <v-select
                  v-model="page.conversionPolicy.outbound[feature.key]"
                  density="compact"
                  hide-details
                  :items="page.runtimeConversionModes"
                  variant="outlined"
                />
              </td>
            </tr>
          </tbody>
        </v-table>

        <h3 class="remote-outbounds__dialog-title">
          <v-icon icon="mdi-laptop" size="18" />
          <span>{{ $t('remoteOutbound.clientConversion') }}</span>
        </h3>
        <v-table density="compact" class="remote-outbounds__policy-table">
          <thead>
            <tr>
              <th>{{ $t('remoteOutbound.sourceFeature') }}</th>
              <th>sing-box</th>
              <th>Xray</th>
              <th>Mihomo</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="feature in page.conversionFeatures" :key="`client-${feature.key}`">
              <td>{{ feature.label }}</td>
              <td>
                <v-select
                  v-model="page.conversionPolicy.client.singBox[feature.key]"
                  density="compact"
                  hide-details
                  :items="page.runtimeConversionModes"
                  variant="outlined"
                />
              </td>
              <td>
                <span
                  v-if="page.isNativeClientConversion(feature.key, 'xray')"
                  class="remote-outbounds__conversion-original"
                >
                  original
                </span>
                <v-select
                  v-else
                  v-model="page.conversionPolicy.client.xray[feature.key]"
                  density="compact"
                  hide-details
                  :items="page.clientConversionModesFor(feature.key, 'xray')"
                  variant="outlined"
                />
              </td>
              <td>
                <span
                  v-if="page.isNativeClientConversion(feature.key, 'mihomo')"
                  class="remote-outbounds__conversion-original"
                >
                  original
                </span>
                <v-select
                  v-else
                  v-model="page.conversionPolicy.client.mihomo[feature.key]"
                  density="compact"
                  hide-details
                  :items="page.clientConversionModesFor(feature.key, 'mihomo')"
                  variant="outlined"
                />
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="page.conversionDialog = false">{{ $t('actions.cancel') }}</v-btn>
        <v-btn color="primary" :loading="page.savingConversionPolicy" @click="page.saveConversionPolicy">
          {{ $t('actions.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="page.collectedDialog" max-width="980">
    <v-card>
      <v-card-title class="remote-outbounds__dialog-heading">
        <v-icon icon="mdi-file-document" size="20" />
        <span>{{ $t('remoteOutbound.collectedData') }}</span>
      </v-card-title>
      <v-card-subtitle v-if="page.collectedData">{{ page.collectedData.name }}</v-card-subtitle>
      <v-card-text>
        <v-progress-linear v-if="page.collectedLoading" indeterminate />
        <RemoteSubscriptionProfile v-else :data="page.collectedData" />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="page.collectedDialog = false">{{ $t('actions.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import RemoteSubscriptionProfile from '@/components/remote/RemoteSubscriptionProfile.vue'

defineProps<{
  page: Record<string, any>
}>()
</script>

<style scoped lang="scss" src="../../views/RemoteOutboundSubscriptions.scss"></style>
