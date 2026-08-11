<template>
  <section aria-labelledby="udp-direct-title">
    <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-3"><h3 id="udp-direct-title">{{ t('serverProtection.udp.title') }}</h3><v-btn variant="tonal" prepend-icon="mdi-refresh" :loading="loading" @click="load(true)">{{ t('serverProtection.udp.refresh') }}</v-btn></div>
    <v-alert type="warning" variant="tonal" class="mb-3">{{ t('serverProtection.udp.experimental') }}</v-alert>
    <v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.udp.boundary') }}</v-alert>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <v-card v-for="capability in status?.capabilities || []" :key="capability.resourceId" variant="outlined" class="mb-3">
      <v-card-title class="d-flex flex-wrap ga-2 align-center"><code>{{ capability.resourceId }}</code><v-chip size="x-small">{{ capability.inboundType }}</v-chip><v-chip size="x-small">{{ capability.strategyClass }}</v-chip><v-chip size="x-small" :color="capability.shippingStatus === 'SHIP' ? 'warning' : 'grey'">{{ capability.shippingStatus }}</v-chip><v-chip size="x-small" :color="stateColor(capability.actualState)">{{ capability.actualState }}</v-chip><v-chip size="x-small">{{ capability.applyGate }}</v-chip></v-card-title>
      <v-card-text><div class="d-flex flex-wrap ga-2 mb-2"><v-chip size="x-small">{{ capability.effectiveNetworks.join(' + ') }}</v-chip><v-chip size="x-small">QUIC build: {{ capability.buildFeatureState }}</v-chip><v-chip size="x-small">auth: {{ yesNo(capability.authenticationPresent) }}</v-chip><v-chip size="x-small">TLS: {{ yesNo(capability.tlsPresent) }}</v-chip><v-chip size="x-small">0-RTT: {{ yesNo(capability.protocolOwnedZeroRtt) }}</v-chip><v-chip size="x-small">migration: {{ yesNo(capability.protocolOwnedMigration) }}</v-chip></div><p v-if="capability.dependentAssociation" class="text-body-2">{{ t('serverProtection.udp.association') }}</p><div v-for="reason in capability.reasonCodes || []" :key="reason" class="text-caption text-warning">{{ reason }}</div></v-card-text>
    </v-card>
    <v-card v-for="plan in status?.plans || []" :key="plan.planId" variant="outlined" class="mb-3">
      <v-card-title class="d-flex flex-wrap ga-2 align-center"><span>{{ t('serverProtection.udp.exactPlan') }}</span><v-chip size="x-small">UDP / {{ plan.claim.addressFamily }}</v-chip><v-chip size="x-small">{{ t('serverProtection.native.desired') }}: {{ plan.desiredPolicy }}</v-chip><v-chip size="x-small">{{ t('serverProtection.native.selected') }}: {{ plan.selectedStrategy }}</v-chip><v-chip size="x-small" :color="stateColor(plan.actualState)">{{ t('serverProtection.native.actual') }}: {{ plan.actualState }}</v-chip><v-chip size="x-small">{{ plan.applyGate }}</v-chip></v-card-title>
      <v-card-text><div><code>{{ plan.claim.configuredBind }}:{{ plan.claim.port }}</code> В· {{ plan.claim.exposure }}</div><div class="text-caption mt-1">{{ plan.flowPolicy.rateProfile }} В· {{ plan.flowPolicy.cardinalityProfile }} В· {{ plan.flowPolicy.icmpPolicy }}</div><v-alert v-if="plan.recoveryRequired" type="error" variant="tonal" density="compact" class="mt-2">RECOVERY_REQUIRED</v-alert><v-alert v-for="warning in plan.warningCodes || []" :key="warning" type="info" variant="tonal" density="compact" class="mt-2">{{ warning }}</v-alert><v-alert v-for="block in plan.blockCodes || []" :key="block" type="warning" variant="tonal" density="compact" class="mt-2">{{ block }}</v-alert><div class="d-flex flex-wrap ga-2 mt-3"><v-btn variant="tonal" @click="preview(plan)">{{ t('serverProtection.udp.preview') }}</v-btn><v-btn color="warning" variant="tonal" :disabled="!udpCanPrepare(plan)" @click="openAction('prepare',plan)">{{ t('serverProtection.udp.prepare') }}</v-btn><v-btn v-if="canResumeApply(plan)" color="error" variant="tonal" @click="openAction('apply',plan)">{{ t('serverProtection.udp.apply') }}</v-btn><v-btn v-if="canResumeRollback(plan)" variant="tonal" @click="openAction('rollback',plan)">{{ t('serverProtection.udp.rollback') }}</v-btn></div></v-card-text>
    </v-card>
    <v-alert v-if="status && !status.capabilities.length" type="info" variant="tonal">{{ t('serverProtection.udp.empty') }}</v-alert>
    <v-dialog v-model="confirmOpen" max-width="680" persistent><v-card><v-card-title>{{ t(`serverProtection.udp.${action}`) }}</v-card-title><v-card-text><v-checkbox v-model="riskAcknowledged" :label="t('serverProtection.udp.ack')" /><p class="text-caption"><code>{{ expectedConfirmation }}</code></p><v-text-field v-model="confirmation" autofocus :label="t('serverProtection.udp.confirmation')" :aria-label="t('serverProtection.udp.confirmation')" /></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="confirmOpen=false">{{ t('serverProtection.udp.cancel') }}</v-btn><v-btn color="warning" :disabled="!riskAcknowledged || confirmation !== expectedConfirmation" @click="confirmAction">{{ t(`serverProtection.udp.${action}`) }}</v-btn></v-card-actions></v-card></v-dialog>
  </section>
</template>

<script setup lang="ts">
import { udpCanPrepare } from '../udpGuardLogic'
import { stateColor } from '../useServerProtection'
import { useUDPDirectGuard } from '../useUDPDirectGuard'

const {
  t, status, loading, error, action, confirmOpen, riskAcknowledged, confirmation,
  expectedConfirmation, yesNo, load, preview, canResumeApply, canResumeRollback,
  openAction, confirmAction,
} = useUDPDirectGuard()
</script>
