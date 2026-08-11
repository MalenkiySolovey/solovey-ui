<template>
  <section aria-labelledby="interception-title">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-3">
      <div>
        <h3 id="interception-title">{{ t('serverProtection.interception.title') }}</h3>
        <p class="text-medium-emphasis mb-0">{{ t('serverProtection.interception.boundary') }}</p>
      </div>
      <v-btn
        prepend-icon="mdi-refresh"
        variant="tonal"
        :loading="loading"
        @click="refresh"
      >
        {{ t('serverProtection.interception.refresh') }}
      </v-btn>
    </div>

    <v-alert type="warning" variant="tonal" class="mb-3">
      <strong>{{ t('serverProtection.interception.experimental') }}</strong>
      <div>{{ t('serverProtection.interception.nonActionable') }}</div>
    </v-alert>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>

    <div v-if="status" class="protection-metric-grid mb-4">
      <v-card variant="outlined">
        <v-card-text>
          <div class="text-caption">{{ t('serverProtection.interception.mutation') }}</div>
          <v-chip size="small" color="error">{{ status.mutationAvailable ? 'AVAILABLE' : 'NOT_SHIPPED' }}</v-chip>
        </v-card-text>
      </v-card>
      <v-card variant="outlined">
        <v-card-text>
          <div class="text-caption">{{ t('serverProtection.interception.allocator') }}</div>
          <div class="text-body-2">{{ status.allocatorState }}</div>
        </v-card-text>
      </v-card>
      <v-card variant="outlined">
        <v-card-text>
          <div class="text-caption">{{ t('serverProtection.interception.health') }}</div>
          <div class="text-body-2">{{ status.healthState }}</div>
        </v-card-text>
      </v-card>
    </div>

    <v-card v-if="status" variant="outlined" class="mb-4">
      <v-card-title>{{ t('serverProtection.interception.matrix') }}</v-card-title>
      <v-table density="compact">
        <thead>
          <tr>
            <th>{{ t('serverProtection.interception.kind') }}</th>
            <th>{{ t('serverProtection.interception.network') }}</th>
            <th>{{ t('serverProtection.interception.family') }}</th>
            <th>{{ t('serverProtection.interception.disposition') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="entry in status.profileMatrix" :key="`${entry.kind}:${entry.network}:${entry.addressFamily}`">
            <td>{{ entry.kind }}</td>
            <td>{{ entry.network.toUpperCase() }}</td>
            <td>{{ entry.addressFamily }}</td>
            <td><v-chip size="x-small" :color="dispositionColor(entry.disposition)">{{ entry.disposition }}</v-chip></td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-alert v-if="status && !status.resources.length" type="info" variant="tonal" class="mb-4">
      {{ t('serverProtection.interception.empty') }}
    </v-alert>
    <v-card
      v-for="resource in status?.resources || []"
      :key="resource.fact.endpointId"
      variant="outlined"
      class="mb-3"
    >
      <v-card-title class="d-flex flex-wrap align-center ga-2">
        <span>{{ resource.fact.inboundTag || resource.fact.resourceId }}</span>
        <v-chip size="x-small">{{ resource.fact.kind }}</v-chip>
        <v-chip size="x-small">{{ resource.fact.network.toUpperCase() }}</v-chip>
        <v-chip size="x-small">{{ resource.fact.addressFamily }}</v-chip>
        <v-chip size="x-small" :color="dispositionColor(resource.disposition)">{{ resource.disposition }}</v-chip>
      </v-card-title>
      <v-card-text>
        <dl class="interception-facts">
          <div><dt>{{ t('serverProtection.interception.ingress') }}</dt><dd>{{ t('serverProtection.interception.noOwnedScope') }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.listener') }}</dt><dd>{{ resource.fact.listenerState }} / {{ resource.fact.ownership }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.originalDestination') }}</dt><dd>{{ resource.fact.originalDestinationMechanism }} / {{ yesNo(resource.fact.originalDestinationPreserved) }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.source') }}</dt><dd>{{ yesNo(resource.fact.sourcePreserved) }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.policyRouting') }}</dt><dd>{{ yesNo(resource.fact.policyRoutingRequired) }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.udpState') }}</dt><dd>{{ yesNo(resource.fact.boundedUdpFlowState) }}</dd></div>
          <div><dt>{{ t('serverProtection.interception.expiry') }}</dt><dd>{{ new Date(resource.fact.expiresAt * 1000).toLocaleString() }}</dd></div>
        </dl>
        <ul class="mt-3">
          <li v-for="reason in resource.reasonCodes" :key="reason" class="text-caption">{{ reason }}</li>
        </ul>
      </v-card-text>
      <v-card-actions>
        <v-btn
          variant="tonal"
          prepend-icon="mdi-file-eye-outline"
          :loading="previewing === resource.fact.endpointId"
          @click="preview(resource)"
        >
          {{ t('serverProtection.interception.preview') }}
        </v-btn>
      </v-card-actions>
    </v-card>

    <v-card v-if="plan" variant="outlined" class="mt-4">
      <v-card-title>{{ t('serverProtection.interception.previewTitle') }}</v-card-title>
      <v-card-text>
        <div class="d-flex flex-wrap ga-2 mb-3">
          <v-chip :color="dispositionColor(plan.disposition)">{{ plan.disposition }}</v-chip>
          <v-chip>{{ plan.desiredState }}</v-chip>
          <v-chip>{{ plan.selectedState }}</v-chip>
          <v-chip>{{ plan.actualState }}</v-chip>
        </div>
        <v-alert type="info" variant="tonal" class="mb-3">
          {{ t('serverProtection.interception.zeroWrite') }}
        </v-alert>
        <p class="text-body-2 mb-2">{{ t('serverProtection.interception.allocationsWithheld') }}</p>
        <ul>
          <li v-for="reason in plan.reasonCodes" :key="reason" class="text-caption">{{ reason }}</li>
        </ul>
      </v-card-text>
    </v-card>

    <v-card v-if="status?.ingressScopes.length" variant="outlined" class="mt-4">
      <v-card-title>{{ t('serverProtection.interception.observedScopes') }}</v-card-title>
      <v-list density="compact">
        <v-list-item
          v-for="scope in status.ingressScopes"
          :key="`${scope.scopeId}:${scope.addressFamily}`"
          :title="`${scope.interfaceName} / ${scope.addressFamily}`"
          :subtitle="`${scope.ownership} · ${scope.forwardedIngress ? 'FORWARDED' : 'NON_ACTIONABLE'} · ${scope.scopeId}`"
        />
      </v-list>
    </v-card>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { protectionAPI } from '../api'
import type {
  InterceptionDisposition,
  InterceptionPlan,
  InterceptionResourceStatus,
  InterceptionStatus,
} from '../interceptionTypes'

const { t } = useI18n()
const status = ref<InterceptionStatus>()
const plan = ref<InterceptionPlan>()
const loading = ref(false)
const previewing = ref('')
const error = ref('')
const liveMessage = ref('')

const dispositionColor = (value: InterceptionDisposition) => {
  if (value === 'SHIP') return 'success'
  if (value === 'NOT_SHIPPED' || value === 'EXTERNAL_MANAGED') return 'error'
  return 'warning'
}

const yesNo = (value: boolean) =>
  value ? t('serverProtection.interception.yes') : t('serverProtection.interception.no')

const refresh = async () => {
  loading.value = true
  error.value = ''
  try {
    status.value = await protectionAPI.interceptionStatus()
    plan.value = undefined
    liveMessage.value = t('serverProtection.interception.statusUpdated')
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : String(caught)
    liveMessage.value = error.value
  } finally {
    loading.value = false
  }
}

const preview = async (resource: InterceptionResourceStatus) => {
  previewing.value = resource.fact.endpointId
  error.value = ''
  try {
    plan.value = await protectionAPI.interceptionPreview(resource.reference)
    liveMessage.value = t('serverProtection.interception.previewReady')
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : String(caught)
    liveMessage.value = error.value
  } finally {
    previewing.value = ''
  }
}

onMounted(refresh)
</script>

<style scoped>
.interception-facts {
  display: grid;
  gap: 0.5rem;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
}
.interception-facts div {
  border-inline-start: 2px solid rgb(var(--v-theme-outline-variant));
  padding-inline-start: 0.75rem;
}
.interception-facts dt {
  color: rgb(var(--v-theme-on-surface-variant));
  font-size: 0.75rem;
}
.interception-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.sr-only {
  height: 1px;
  margin: -1px;
  overflow: hidden;
  padding: 0;
  position: absolute;
  width: 1px;
}
</style>
