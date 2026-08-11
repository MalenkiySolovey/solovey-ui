<template>
  <div>
    <v-alert type="info" variant="tonal" class="mb-3" icon="mdi-eye-outline">
      {{ t('serverProtection.contracts.inspectOnly') }}
    </v-alert>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>

    <v-card variant="outlined" class="mb-4">
      <v-card-title class="d-flex flex-wrap align-center ga-2">
        firewall baseline endpoint strategy
        <v-chip size="small" :color="firewallBaseline.firewallBaselineEligibility.candidateEligible ? 'success' : 'warning'">{{ firewallBaseline.status.selected }}</v-chip>
        <v-chip size="small" color="info">actual: {{ firewallBaseline.status.actual }}</v-chip>
        <v-chip size="small" color="warning">real nftables Live: {{ firewallBaseline.realNftablesLive }}</v-chip>
      </v-card-title>
      <v-card-text>
		<div class="text-caption mb-2">Snapshot <code>{{ firewallBaseline.snapshotBinding.revision || 'unknown' }}</code> · graph <code>{{ firewallBaseline.socketGraph.revision || 'unknown' }}</code> · kernel plan <code>{{ firewallBaseline.kernelPlan.revision || 'unknown' }}</code></div>
		<div class="text-caption mb-2">Candidate <code>{{ firewallBaseline.snapshotBinding.candidateSha256 || 'unknown' }}</code> · capability {{ firewallBaseline.capabilityAssessment.advancedState }} / {{ firewallBaseline.capabilityAssessment.acceptanceConsequence }}</div>
        <v-alert v-for="reason in firewallBaseline.firewallBaselineEligibility.reasonCodes || []" :key="reason" type="warning" variant="tonal" class="mb-2">{{ reason }}</v-alert>
		<v-alert v-for="reason in firewallBaseline.firewallBaselineEligibility.advisoryCodes || []" :key="`advisory:${reason}`" type="info" variant="tonal" class="mb-2">owner advisory: {{ reason }}</v-alert>
        <v-table density="compact">
          <thead><tr><th>Resource</th><th>Recommendation</th><th>Socket ownership</th><th>Desired / selected / actual</th><th>Exact revisions</th></tr></thead>
          <tbody>
            <tr v-for="node in firewallBaseline.socketGraph.nodes" :key="node.resourceId">
              <td><code>{{ node.resourceId }}</code></td>
              <td>{{ node.selectedStrategy }}<div v-for="reason in node.reasonCodes || []" :key="reason" class="text-caption text-warning">{{ reason }}</div></td>
              <td>{{ node.observedClaims.map(claim => claim.owner || 'unknown').join(', ') || 'unknown' }}<div class="text-caption">desired {{ node.desiredClaims.length }} / observed {{ node.observedClaims.length }}</div></td>
              <td>{{ endpointStatus(node.resourceId) }}</td>
              <td class="text-caption"><div>owner: <code>{{ node.ownerRevision || 'unknown' }}</code></div><div>config: <code>{{ node.configurationRevision || 'unknown' }}</code></div><div>endpoint: <code>{{ endpointRevision(node.resourceId) }}</code></div></td>
            </tr>
          </tbody>
        </v-table>
        <v-alert v-for="collision in firewallBaseline.socketGraph.collisions" :key="`${collision.leftResourceId}:${collision.rightResourceId}:${collision.code}`" type="warning" variant="tonal" class="mt-2">
          {{ collision.code }}: {{ collision.leftResourceId }} / {{ collision.rightResourceId }}
          <div v-for="alternative in collision.alternatives" :key="alternative.code" class="text-caption">{{ alternative.code }} — {{ alternative.detail }}</div>
        </v-alert>
      </v-card-text>
    </v-card>

    <v-card variant="outlined" class="mb-4">
      <v-card-title>{{ t('serverProtection.contracts.endpointFacts') }}</v-card-title>
      <v-table density="compact">
        <thead><tr><th>Intent</th><th>Network</th><th>Bind</th><th>Owner / source</th><th>Confidence</th><th>Age / state</th></tr></thead>
        <tbody>
          <template v-for="resource in inventory.resources" :key="resource.id">
            <tr v-for="endpoint in resource.endpoints || []" :key="endpoint.id">
              <td>{{ endpoint.intent }}</td><td>{{ endpoint.key.network }} / {{ endpoint.key.addressFamily }}</td>
              <td><code>{{ endpoint.key.bindAddress }}:{{ endpoint.key.port || '?' }}</code></td><td>{{ endpoint.owner }} / {{ endpoint.source }}</td>
              <td>{{ endpoint.confidenceBp }} bp</td><td>{{ factAge(endpoint.observedAt) }} / {{ factState({ reasonCodes: endpoint.reasonCodes }) }}</td>
            </tr>
          </template>
        </tbody>
      </v-table>
    </v-card>

    <v-card variant="outlined" class="mb-4">
      <v-card-title class="d-flex align-center ga-2">{{ t('serverProtection.contracts.hostSurfaces') }} <v-chip v-if="hostSurfaces.truncated" size="x-small" color="warning">truncated</v-chip></v-card-title>
      <v-alert v-if="!hostSurfaces.items.length" type="info" variant="tonal" class="ma-3">unknown</v-alert>
      <v-table v-else density="compact"><thead><tr><th>Exposure</th><th>Socket</th><th>Classification</th><th>Owner / source</th><th>Confidence</th><th>Age / state</th></tr></thead><tbody>
        <tr v-for="fact in hostSurfaces.items" :key="fact.id"><td>{{ fact.exposure }}</td><td>{{ fact.network }} / {{ fact.family }} <code>{{ fact.bind }}:{{ fact.port || '?' }}</code></td><td>{{ fact.classification }}</td><td>{{ fact.desiredOwner || 'unknown' }} / {{ fact.source }}</td><td>{{ fact.confidenceBp }} bp</td><td>{{ factAge(fact.lastSeen) }} / {{ factState(fact) }}</td></tr>
      </tbody></v-table>
    </v-card>

    <v-row>
      <v-col cols="12" lg="6">
        <v-card variant="outlined" height="100%">
          <v-card-title>{{ t('serverProtection.contracts.targets') }}</v-card-title>
          <v-list density="compact">
            <v-list-subheader>{{ t('serverProtection.native.actionableTargets') }}</v-list-subheader>
            <v-list-item v-for="target in targets.targetsV2" :key="`${target.identity.providerId}:${target.identity.targetId}:${target.reference.endpointRevision}`"
              :title="`${target.identity.providerId} / ${target.identity.targetId}`"
              :subtitle="`${target.endpointMode} · ${target.health.state} · ${target.capacity.state}`">
              <template #append><v-chip size="x-small" :color="target.actionable ? 'success' : 'warning'">{{ target.actionable ? t('serverProtection.native.actionable') : t('serverProtection.native.blocked') }}</v-chip></template>
            </v-list-item>
            <v-list-subheader>{{ t('serverProtection.native.legacyTargets') }}</v-list-subheader>
            <v-list-item v-for="target in targets.items" :key="`legacy:${target.identity.providerId}:${target.identity.targetId}`"
              :title="`${target.identity.providerId} / ${target.identity.targetId}`"
              :subtitle="`${target.readiness} · ${target.endpointId} · ${factAge(target.observedAt)}`">
              <template #append><v-chip size="x-small" color="grey">{{ t('serverProtection.native.nonActionable') }}</v-chip></template>
            </v-list-item>
            <v-list-item v-if="!targets.items.length && !targets.targetsV2.length" :title="t('serverProtection.native.unknown')" />
          </v-list>
        </v-card>
      </v-col>
      <v-col cols="12" lg="6"><v-card variant="outlined" height="100%"><v-card-title>{{ t('serverProtection.contracts.managementRecovery') }}</v-card-title><v-card-text><v-chip class="mb-2" size="small" :color="posture.recoveryState === 'fresh_independent_path_present' ? 'success' : 'warning'">{{ posture.recoveryState }}</v-chip><div v-for="capability in posture.capabilities" :key="capability.kind" class="text-body-2 mb-1">{{ capability.kind }}: {{ capability.state }}</div><div v-for="recovery in posture.recoveryPaths" :key="recovery.id" class="text-caption mt-2"><code>{{ recovery.endpointId }}</code> · {{ recovery.verificationMethod }} · {{ recovery.independenceClass }} · {{ recovery.verificationState }} · expires {{ factAge(recovery.expiresAt) }}</div><div class="text-caption mt-2">Implemented: {{ posture.implemented }}</div><div class="text-caption">Planned: {{ posture.planned }}</div></v-card-text></v-card></v-col>
    </v-row>

    <v-row class="mt-1">
      <v-col cols="12" lg="6"><v-card variant="outlined" height="100%"><v-card-title>ProtectionSignalV2</v-card-title><v-list density="compact"><v-list-item v-for="signal in signals.items" :key="signal.signalId" :title="`${signal.category} / ${signal.knownKind ? signal.kind : 'unknown'}`" :subtitle="`${signal.scope.scope} · ${signal.source.sourceId} · ${signal.confidenceBp} bp · ${factAge(signal.observedAt)} / ${factState(signal)}`" /><v-list-item v-if="!signals.items.length" title="No bounded signals stored" /></v-list></v-card></v-col>
      <v-col cols="12" lg="6"><v-card variant="outlined" height="100%"><v-card-title>ProtectionDecisionV2</v-card-title><v-list density="compact"><v-list-item v-for="decision in decisions.items" :key="decision.decisionId" :title="`${decision.state} / ${decision.requestedIntent}`" :subtitle="`${decision.scope.scope} · resolved ${decision.capabilityResolution.resolvedIntent} · capability implemented: ${decision.capabilityResolution.implemented}`" /><v-list-item v-if="!decisions.items.length" title="No scoped decisions stored" /></v-list><v-alert type="info" variant="tonal" class="ma-3">A decision is not an applied action. Resolver previews remain NOT_APPLIED until exact helper verification.</v-alert></v-card></v-col>
    </v-row>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { protectionAPI } from '../api'
import { factAge, factState } from '../contractInspectionLogic'
import type { DecisionV2, HostSurfaceFact, Inventory, FirewallBaselineSnapshot, NativeTargetInspection, PostureFacts, SignalV2 } from '../types'

interface Page<T> { items: T[]; total: number; truncated?: boolean }
const { t } = useI18n()
const error = ref('')
const inventory = ref<Inventory>({ generatedAt: 0, resources: [] })
const hostSurfaces = ref<Page<HostSurfaceFact>>({ items: [], total: 0 })
const targets = ref<NativeTargetInspection>({ items: [], targetsV2: [], page: 1, limit: 500, total: 0, totalV2: 0, generatedAt: 0, reservations: [], reservationsTruncated: false })
const signals = ref<Page<SignalV2>>({ items: [], total: 0 })
const decisions = ref<Page<DecisionV2>>({ items: [], total: 0 })
const posture = ref<PostureFacts>({ managementEndpoints: [], recoveryPaths: [], recoveryState: 'unknown', capabilities: [], implemented: 'unknown', planned: 'unknown' })
const firewallBaseline = ref<FirewallBaselineSnapshot>({ recommendations: [], socketGraph: { revision: '', generatedAt: 0, nodes: [], collisions: [], applyBlocked: true }, kernelPlan: { revision: '', inputRevision: '', graphRevision: '', mode: 'COEXISTENCE_ENDPOINT_MANAGED', applyBlocked: true, endpoints: [] }, firewallBaselineEligibility: { kind: 'FIREWALL_BASELINE_ELIGIBILITY', revision: '', candidateEligible: false, mutationReady: false, endpointInventoryComplete: false, managementPreserved: false, exactRevisions: false, managedTableOnly: true, noForeignMutation: true }, listenerTopologyMutationEligibility: { kind: 'LISTENER_TOPOLOGY_MUTATION_ELIGIBILITY', revision: '', eligible: false, graphRevision: '' }, kernelPreview: { revision: '', inputRevision: '', backend: 'preview_only', wouldKeep: [], wouldOpen: [], wouldWarn: [], wouldBlock: [], warnings: [] }, snapshotBinding: { schema: '', revision: '', runtimeRevision: '', resourceRevision: '', graphRevision: '', configurationRevision: '', policyRevision: '', recoveryRevision: '', planRevision: '', candidateSha256: '', capturedAt: 0 }, capabilityAssessment: { ttlRequired: false, ttlSupported: false, rateRequired: false, rateSupported: false, candidateSupported: false, advancedState: 'DEFERRED_UNPROVEN', acceptanceConsequence: 'BASELINE_BLOCKED', sshRecoverySupported: false }, managementGuard: { state: 'unknown', invalidRecoveryRecords: 0, recoveryPaths: [] }, status: { desired: 'COEXISTENCE_ENDPOINT_MANAGED', selected: 'OBSERVE_ONLY', actual: 'NOT_APPLIED' }, realNftablesLive: 'NOT_RUN', stabilityClaim: 'normal_ci_only' })

const endpointPlan = (resourceId: string) => firewallBaseline.value.kernelPlan.endpoints.find(endpoint => endpoint.resourceId === resourceId)
const endpointRevision = (resourceId: string) => endpointPlan(resourceId)?.endpointRevision || 'unknown'
const endpointStatus = (resourceId: string) => {
  const endpoint = endpointPlan(resourceId)
  return endpoint ? `${endpoint.desiredStatus} / ${endpoint.selectedStatus} / ${endpoint.actualStatus}` : 'unknown / observe-only / NOT_APPLIED'
}

onMounted(async () => {
  try {
    [inventory.value, hostSurfaces.value, targets.value, signals.value, decisions.value, posture.value, firewallBaseline.value] = await Promise.all([
      protectionAPI.get<Inventory>('/resources', { limit: 500 }),
      protectionAPI.get<Page<HostSurfaceFact>>('/host-surfaces', { limit: 500 }),
      protectionAPI.nativeFallbackTargets(1, 500),
      protectionAPI.get<Page<SignalV2>>('/signals', { limit: 100 }),
      protectionAPI.get<Page<DecisionV2>>('/decisions', { limit: 100 }),
      protectionAPI.get<PostureFacts>('/posture', { limit: 100 }),
	  protectionAPI.get<FirewallBaselineSnapshot>('/firewall-baseline'),
    ])
  } catch (reason) { error.value = reason instanceof Error ? reason.message : String(reason) }
})
</script>
