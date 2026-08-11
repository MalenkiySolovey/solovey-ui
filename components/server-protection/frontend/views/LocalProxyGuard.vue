<template>
  <section aria-labelledby="local-proxy-title">
    <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-3">
      <h3 id="local-proxy-title">{{ t('serverProtection.localProxy.title') }}</h3>
      <v-btn variant="tonal" prepend-icon="mdi-refresh" :loading="loading" @click="load(true)">{{ t('serverProtection.localProxy.refresh') }}</v-btn>
    </div>
    <v-alert type="warning" variant="tonal" class="mb-3">{{ t('serverProtection.localProxy.experimental') }}</v-alert>
    <v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.localProxy.boundary') }}</v-alert>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>

    <v-card v-for="plan in status?.plans || []" :key="plan.resourceId + plan.endpointId" variant="outlined" class="mb-3">
      <v-card-title class="d-flex flex-wrap align-center ga-2">
        <code>{{ plan.resourceId }}</code>
        <v-chip size="x-small">{{ plan.fact.inboundType.toUpperCase() }}</v-chip>
        <v-chip size="x-small" :color="stateColor(plan.actualState)">{{ plan.actualState }}</v-chip>
        <v-chip size="x-small" :color="plan.applyGate === 'BLOCKED' ? 'grey' : 'warning'">{{ plan.applyGate }}</v-chip>
      </v-card-title>
      <v-card-text>
        <div class="d-flex flex-wrap ga-2 mb-2">
          <v-chip size="x-small">{{ plan.fact.protocols.join(' + ') }}</v-chip>
          <v-chip size="x-small">exposure: {{ plan.fact.exposure }}</v-chip>
          <v-chip size="x-small">owner: {{ plan.fact.ownership }}</v-chip>
          <v-chip size="x-small">auth: {{ plan.fact.authentication }}</v-chip>
          <v-chip size="x-small">TLS: {{ plan.fact.tls }}</v-chip>
        </div>
        <v-table density="compact" class="mb-2">
          <thead><tr><th>{{ t('serverProtection.localProxy.listener') }}</th><th>{{ t('serverProtection.localProxy.bind') }}</th><th>{{ t('serverProtection.localProxy.port') }}</th><th>{{ t('serverProtection.localProxy.identity') }}</th></tr></thead>
          <tbody>
            <tr><td>{{ t('serverProtection.localProxy.configured') }}</td><td><code>{{ plan.fact.configuredBind }}</code></td><td>{{ plan.fact.configuredPort }}</td><td>{{ plan.fact.addressFamily }}</td></tr>
            <tr><td>{{ t('serverProtection.localProxy.observed') }}</td><td><code>{{ plan.fact.observedBind || '—' }}</code></td><td>{{ plan.fact.observedPort || '—' }}</td><td>{{ plan.fact.listenerState }}</td></tr>
          </tbody>
        </v-table>
        <div class="text-caption">{{ t('serverProtection.localProxy.factExpires') }}: {{ new Date(plan.fact.expiresAt * 1000).toLocaleString() }}</div>
        <v-alert v-if="plan.fact.dependentUdpAssociation" type="info" density="compact" variant="tonal" class="mt-2">{{ t('serverProtection.localProxy.udpAssociation') }}</v-alert>
        <template v-if="stateFor(plan)">
          <div class="d-flex flex-wrap ga-2 mt-3">
            <v-chip size="x-small">lease: {{ stateFor(plan)?.lease.state }}</v-chip>
            <v-chip size="x-small">health: {{ stateFor(plan)?.health.length || 0 }}/{{ plan.fact.protocols.length }}</v-chip>
            <v-chip size="x-small">operation: {{ stateFor(plan)?.latestOperationRevision }}</v-chip>
          </div>
          <div v-for="health in stateFor(plan)?.health || []" :key="health.protocol" class="text-caption mt-1">
            {{ health.protocol }} · {{ health.passed ? 'PASS' : 'FAIL' }} · generation {{ health.generation }}
          </div>
          <v-alert v-if="stateFor(plan)?.recoveryRequired" type="error" density="compact" variant="tonal" class="mt-2">RECOVERY_REQUIRED</v-alert>
        </template>
        <v-alert v-for="warning in plan.warningCodes || []" :key="warning" type="info" density="compact" variant="tonal" class="mt-2">{{ warning }}</v-alert>
        <v-alert v-for="block in plan.blockCodes || []" :key="block" type="warning" density="compact" variant="tonal" class="mt-2">{{ block }}</v-alert>
        <div class="d-flex flex-wrap ga-2 mt-3">
          <v-btn variant="tonal" @click="preview(plan)">{{ t('serverProtection.localProxy.preview') }}</v-btn>
          <v-btn color="warning" variant="tonal" :disabled="!localProxyCanPrepare(plan, stateFor(plan))" @click="openAction('prepare', plan)">{{ t('serverProtection.localProxy.prepare') }}</v-btn>
          <v-btn v-if="localProxyCanApply(stateFor(plan))" color="error" variant="tonal" @click="openAction('apply', plan)">{{ t('serverProtection.localProxy.apply') }}</v-btn>
          <v-btn v-if="localProxyCanDisable(stateFor(plan))" variant="tonal" @click="openAction('disable', plan)">{{ t('serverProtection.localProxy.disable') }}</v-btn>
        </div>
      </v-card-text>
    </v-card>
    <v-alert v-if="status && !status.plans.length" type="info" variant="tonal">{{ t('serverProtection.localProxy.empty') }}</v-alert>

    <v-dialog v-model="confirmOpen" max-width="680" persistent>
      <v-card>
        <v-card-title>{{ t(`serverProtection.localProxy.${action}`) }}</v-card-title>
        <v-card-text>
          <v-checkbox v-if="action !== 'disable'" v-model="acknowledged" :label="t('serverProtection.localProxy.ack')" />
          <p class="text-caption"><code>{{ expectedConfirmation }}</code></p>
          <v-text-field v-model="confirmation" autofocus :label="t('serverProtection.localProxy.confirmation')" />
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="confirmOpen=false">{{ t('serverProtection.localProxy.cancel') }}</v-btn><v-btn color="warning" :disabled="(action !== 'disable' && !acknowledged) || confirmation !== expectedConfirmation" @click="confirmAction">{{ t(`serverProtection.localProxy.${action}`) }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<script setup lang="ts">
import { stateColor } from '../useServerProtection'
import { useLocalProxyGuard } from '../useLocalProxyGuard'

const {
  t, status, action, loading, error, confirmOpen, acknowledged, confirmation, expectedConfirmation,
  stateFor, load, preview, openAction, confirmAction, localProxyCanPrepare, localProxyCanApply, localProxyCanDisable,
} = useLocalProxyGuard()
</script>
