<template>
	<v-card variant="outlined">
		<v-card-title>{{ t('serverProtection.fronting.title') }}</v-card-title>
		<v-card-text>
			<v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.fronting.manualOnly') }}</v-alert>
			<v-alert type="warning" variant="tonal" class="mb-3">{{ t('serverProtection.fronting.alpnNotShipped') }}</v-alert>
			<v-alert v-if="errorCode" type="error" variant="tonal" class="mb-3" role="alert">{{ reasonLabel(errorCode) }}</v-alert>

			<div class="d-flex flex-wrap ga-2 mb-3">
				<v-btn prepend-icon="mdi-refresh" variant="tonal" :loading="busy" @click="refresh">{{ t('serverProtection.fronting.refresh') }}</v-btn>
				<v-chip color="warning">{{ t('serverProtection.fronting.applyGate') }}</v-chip>
				<v-chip color="info">{{ t('serverProtection.fronting.httpNotShipped') }}</v-chip>
				<v-chip color="info">{{ t('serverProtection.fronting.udpNotShipped') }}</v-chip>
			</div>

			<v-select
				v-model="draft.resourceId"
				:items="page?.items || []"
				item-title="displayIdentity"
				item-value="resourceId"
				:label="t('serverProtection.fronting.resource')"
				:disabled="busy"
				@update:model-value="invalidatePlan"
			/>

			<section v-if="selectedStatus" aria-labelledby="fronting-runtime-title">
				<h3 id="fronting-runtime-title" class="text-subtitle-1 mb-2">{{ t('serverProtection.fronting.runtimeTitle') }}</h3>
				<div class="d-flex flex-wrap ga-2 mb-3" role="status" aria-live="polite">
					<v-chip :color="stateColor(selectedStatus.runtime.state)">{{ runtimeLabel(selectedStatus.runtime.state) }}</v-chip>
					<v-chip>{{ installationLabel(selectedStatus.runtime.installationClass) }}</v-chip>
					<v-chip>{{ t('serverProtection.fronting.stream') }}: {{ supportLabel(selectedStatus.runtime.stream?.effective) }}</v-chip>
					<v-chip>{{ t('serverProtection.fronting.sslPreread') }}: {{ supportLabel(selectedStatus.runtime.sslPreread?.effective) }}</v-chip>
					<v-chip>{{ t('serverProtection.fronting.actual') }}: {{ stateLabel(selectedStatus.actualState) }}</v-chip>
				</div>
				<v-alert v-if="selectedStatus.runtime.installationClass === 'EXTERNAL_MANAGED'" type="warning" variant="tonal" class="mb-3">
					{{ t('serverProtection.fronting.externalInspectionOnly') }}
				</v-alert>
				<v-alert v-for="reason in statusReasons" :key="reason" type="warning" variant="tonal" class="mb-2">{{ reasonLabel(reason) }}</v-alert>
			</section>

			<v-divider class="my-4" />
			<h3 class="text-subtitle-1 mb-2">{{ t('serverProtection.fronting.editorTitle') }}</h3>
			<v-row>
				<v-col cols="12" md="6">
					<v-select v-model="draft.strategy" :items="strategyItems" item-title="title" item-value="value" :label="t('serverProtection.fronting.strategy')" :disabled="busy" @update:model-value="invalidatePlan" />
				</v-col>
				<v-col cols="12" md="6">
					<v-select v-model="draft.socketClaimRevision" :items="socketItems" item-title="title" item-value="value" :label="t('serverProtection.fronting.socket')" :disabled="busy || !selectedStatus" @update:model-value="invalidatePlan" />
				</v-col>
				<v-col cols="12" md="6">
					<v-select v-model="draft.proxyMode" :items="proxyItems" item-title="title" item-value="value" :label="t('serverProtection.fronting.proxyMode')" :disabled="busy" @update:model-value="invalidatePlan" />
				</v-col>
				<v-col v-if="draft.strategy === 'L4_ONE_TO_ONE_FRONTING'" cols="12" md="6">
					<v-select v-model="draft.defaultTargetOptionId" :items="targetItems" item-title="label" item-value="id" :label="t('serverProtection.fronting.target')" :disabled="busy" @update:model-value="invalidatePlan" />
				</v-col>
			</v-row>

			<div v-if="draft.strategy === 'SNI_PREREAD_FRONTING'">
				<div class="d-flex align-center justify-space-between mb-2">
					<h4 class="text-subtitle-2">{{ t('serverProtection.fronting.sniRoutes') }}</h4>
					<v-btn size="small" variant="tonal" prepend-icon="mdi-plus" :disabled="busy || draft.routes.length >= 32" @click="addRoute">{{ t('serverProtection.fronting.addRoute') }}</v-btn>
				</div>
				<v-row v-for="(route, index) in draft.routes" :key="index" class="align-center">
					<v-col cols="12" md="5"><v-text-field v-model="route.sni" :label="t('serverProtection.fronting.exactSni')" :error-messages="route.sni && !validExactSNI(route.sni) ? [t('serverProtection.fronting.sniInvalid')] : []" @update:model-value="invalidatePlan" /></v-col>
					<v-col cols="10" md="6"><v-select v-model="route.targetOptionId" :items="targetItems" item-title="label" item-value="id" :label="t('serverProtection.fronting.target')" @update:model-value="invalidatePlan" /></v-col>
					<v-col cols="2" md="1"><v-btn icon="mdi-delete-outline" variant="text" :aria-label="t('serverProtection.fronting.removeRoute')" @click="removeRoute(index)" /></v-col>
				</v-row>
				<v-row>
					<v-col cols="12" md="6"><v-select v-model="draft.defaultPolicy" :items="defaultPolicyItems" item-title="title" item-value="value" :label="t('serverProtection.fronting.defaultPolicy')" @update:model-value="invalidatePlan" /></v-col>
					<v-col v-if="draft.defaultPolicy !== 'REJECT'" cols="12" md="6"><v-select v-model="draft.defaultTargetOptionId" :items="targetItems" item-title="label" item-value="id" :label="t('serverProtection.fronting.defaultTarget')" @update:model-value="invalidatePlan" /></v-col>
				</v-row>
			</div>

			<v-alert type="info" variant="tonal" class="my-3">{{ t('serverProtection.fronting.naturalRouting') }}</v-alert>
			<v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.fronting.forcedRoutingUnsupported') }}</v-alert>

			<div class="d-flex flex-wrap ga-2">
				<v-btn color="primary" variant="tonal" prepend-icon="mdi-file-eye-outline" :loading="busy" :disabled="!previewRequest" @click="preview">{{ t('serverProtection.fronting.preview') }}</v-btn>
				<v-btn color="warning" variant="tonal" prepend-icon="mdi-content-save-cog-outline" :disabled="!canPrepareFronting(!!plan, plan?.safety.blocks || []) || busy" @click="openDialog('prepare')">{{ t('serverProtection.fronting.prepare') }}</v-btn>
				<v-btn color="error" variant="tonal" prepend-icon="mdi-play-circle-outline" :disabled="!canApplyFronting(operation?.actualState) || busy" @click="openDialog('apply')">{{ t('serverProtection.fronting.apply') }}</v-btn>
				<v-btn color="warning" variant="tonal" :disabled="!canRollbackFronting(operation?.actualState) || busy" @click="openDialog('rollback')">{{ t('serverProtection.fronting.rollback') }}</v-btn>
			</div>

			<v-card v-if="plan" variant="tonal" class="mt-4" role="status" aria-live="polite">
				<v-card-title>{{ t('serverProtection.fronting.previewTitle') }}</v-card-title>
				<v-card-text>
					<div>{{ t('serverProtection.fronting.desired') }}: <strong>{{ strategyLabel(plan.strategy.desired) }}</strong></div>
					<div>{{ t('serverProtection.fronting.selected') }}: <strong>{{ strategyLabel(plan.strategy.selected) }}</strong></div>
					<div>{{ t('serverProtection.fronting.actual') }}: <strong>{{ stateLabel(plan.strategy.actual) }}</strong></div>
					<div>{{ t('serverProtection.fronting.socket') }}: <code>{{ plan.publicSocket.resourceId }} · {{ plan.publicSocket.endpointId }}</code></div>
					<div>{{ t('serverProtection.fronting.proxyMode') }}: <strong>{{ plan.targets.selectedProxyMode }}</strong></div>
					<div>{{ t('serverProtection.fronting.defaultPolicy') }}: <strong>{{ plan.selectors.default.policy }}</strong></div>
					<div>{{ t('serverProtection.fronting.expires') }}: {{ new Date(plan.expiresAt * 1000).toLocaleString() }}</div>
					<v-alert v-for="reason in plan.safety.reasonCodes" :key="reason" :type="plan.safety.blocks.length ? 'warning' : 'info'" variant="tonal" class="mt-2">{{ reasonLabel(reason) }}</v-alert>
					<v-alert type="warning" variant="tonal" class="mt-2">{{ t('serverProtection.fronting.experimentalWarning') }}</v-alert>
				</v-card-text>
			</v-card>

			<v-card v-if="operation" variant="tonal" class="mt-4" role="status" aria-live="polite">
				<v-card-title class="d-flex flex-wrap align-center ga-2"><code>{{ operation.operationId }}</code><v-chip :color="stateColor(operation.actualState)">{{ stateLabel(operation.actualState) }}</v-chip></v-card-title>
				<v-card-text>
					<div>{{ t('serverProtection.fronting.strategy') }}: {{ strategyLabel(operation.strategy) }}</div>
					<div>{{ t('serverProtection.fronting.health') }}: {{ supportLabel(operation.healthState) }}</div>
					<div>{{ t('serverProtection.fronting.safeNextAction') }}: {{ actionLabel(operation.safeNextAction) }}</div>
					<v-alert v-if="operation.recoveryRequired" type="error" variant="tonal" class="mt-2">{{ t('serverProtection.fronting.recoveryRequired') }}</v-alert>
					<v-alert v-for="reason in operation.reasonCodes" :key="reason" type="warning" variant="tonal" class="mt-2">{{ reasonLabel(reason) }}</v-alert>
				</v-card-text>
			</v-card>

			<v-card v-if="recovery" variant="outlined" class="mt-3">
				<v-card-title>{{ t('serverProtection.fronting.recoveryTitle') }}</v-card-title>
				<v-card-text>
					<div>{{ t('serverProtection.fronting.recoveryClass') }}: {{ recovery.classification || t('serverProtection.fronting.unknown') }}</div>
					<div>{{ t('serverProtection.fronting.safeNextAction') }}: {{ actionLabel(recovery.permittedNextAction) }}</div>
					<div>{{ t('serverProtection.fronting.checkpointRetained') }}: {{ yesNo(recovery.checkpointRetained) }}</div>
					<div>{{ t('serverProtection.fronting.authoritiesRetained') }}: {{ yesNo(recovery.authoritiesRetained) }}</div>
				</v-card-text>
			</v-card>
		</v-card-text>
	</v-card>

	<v-dialog v-model="dialog" max-width="680" persistent>
		<v-card>
			<v-card-title>{{ dialogTitle }}</v-card-title>
			<v-card-text>
				<p class="mb-3">{{ dialogExplanation }}</p>
				<code class="d-block mb-3">{{ expectedConfirmation }}</code>
				<v-text-field v-model="confirmation" autofocus :label="t('serverProtection.fronting.confirmation')" :error-messages="confirmation && confirmation !== expectedConfirmation ? [t('serverProtection.fronting.confirmationMismatch')] : []" />
			</v-card-text>
			<v-card-actions>
				<v-spacer />
				<v-btn variant="text" :disabled="busy" @click="dialog = false">{{ t('serverProtection.fronting.cancel') }}</v-btn>
				<v-btn color="warning" :loading="busy" :disabled="confirmation !== expectedConfirmation" @click="confirmMutation">{{ t('serverProtection.fronting.confirm') }}</v-btn>
			</v-card-actions>
		</v-card>
	</v-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ProtectionAPIError, protectionAPI } from '../api'
import {
	buildFrontingPreviewRequest,
	canApplyFronting,
	canPrepareFronting,
	canRollbackFronting,
	normalizeFrontingState,
	normalizeFrontingStrategy,
	targetOptions,
	validExactSNI,
	type FrontingEditorDraft,
} from '../frontingLogic'
import type { FrontingOperation, FrontingPlan, FrontingRecoveryStatus, FrontingStatusPage } from '../types'
import { stateColor } from '../useServerProtection'

const { t } = useI18n()
const page = ref<FrontingStatusPage>()
const plan = ref<FrontingPlan>()
const operation = ref<FrontingOperation>()
const recovery = ref<FrontingRecoveryStatus>()
const busy = ref(false)
const errorCode = ref('')
const dialog = ref(false)
const dialogAction = ref<'prepare' | 'apply' | 'rollback'>('prepare')
const confirmation = ref('')
const idempotencyKey = ref('')
const draft = reactive<FrontingEditorDraft>({
	resourceId: '', strategy: 'L4_ONE_TO_ONE_FRONTING', socketClaimRevision: '', proxyMode: 'OFF', routes: [],
	defaultPolicy: 'REJECT', defaultTargetOptionId: '',
})

const selectedStatus = computed(() => page.value?.items.find(item => item.resourceId === draft.resourceId))
const targetItems = computed(() => targetOptions(selectedStatus.value))
const socketItems = computed(() => (selectedStatus.value?.socketClaims || []).map(claim => ({
	title: `${claim.resourceId} · ${claim.endpointId} · ${claim.addressFamily} · ${claim.publicPort}`,
	value: claim.claimRevision,
})))
const previewRequest = computed(() => selectedStatus.value ? buildFrontingPreviewRequest(selectedStatus.value, draft) : undefined)
const statusReasons = computed(() => [...new Set([...(selectedStatus.value?.blocks || []), ...(selectedStatus.value?.reasonCodes || [])])])
const strategyItems = computed(() => [
	{ title: t('serverProtection.fronting.strategyL4'), value: 'L4_ONE_TO_ONE_FRONTING' },
	{ title: t('serverProtection.fronting.strategySni'), value: 'SNI_PREREAD_FRONTING' },
])
const defaultPolicyItems = computed(() => [
	{ title: t('serverProtection.fronting.defaultReject'), value: 'REJECT' },
	{ title: t('serverProtection.fronting.defaultFixed'), value: 'FIXED_SAFE_DEFAULT' },
	{ title: t('serverProtection.fronting.defaultNonTls'), value: 'NON_TLS_FIXED_TARGET' },
])
const proxyOnSupported = computed(() => selectedStatus.value?.capabilities.some(capability =>
	capability.strategy === draft.strategy && capability.proxyProtocolEmit?.availability === 'SUPPORTED') ?? false)
const proxyItems = computed(() => [
	{ title: t('serverProtection.fronting.proxyOff'), value: 'OFF' },
	...(proxyOnSupported.value ? [{ title: t('serverProtection.fronting.proxyOn'), value: 'ON' }] : []),
])

const expectedConfirmation = computed(() => {
	if (dialogAction.value === 'prepare') return plan.value ? `PREPARE FRONTING ${plan.value.canonicalPlanDigest}` : ''
	if (dialogAction.value === 'apply') return operation.value ? `APPLY FRONTING ${operation.value.operationId}` : ''
	return operation.value ? `ROLLBACK FRONTING ${operation.value.operationId}` : ''
})
const dialogTitle = computed(() => t(`serverProtection.fronting.dialog${dialogAction.value[0].toUpperCase()}${dialogAction.value.slice(1)}`))
const dialogExplanation = computed(() => t(`serverProtection.fronting.explain${dialogAction.value[0].toUpperCase()}${dialogAction.value.slice(1)}`))

const run = async (action: () => Promise<void>) => {
	busy.value = true
	errorCode.value = ''
	try { await action() } catch (error) {
		errorCode.value = error instanceof ProtectionAPIError ? error.code : 'unknown_error'
	} finally { busy.value = false }
}

const refresh = () => run(async () => {
	const previousResource = draft.resourceId
	page.value = await protectionAPI.frontingStatus()
	if (previousResource && !page.value.items.some(item => item.resourceId === previousResource)) draft.resourceId = ''
	if (operation.value) {
		operation.value = await protectionAPI.frontingOperation(operation.value.operationId)
		if (operation.value.recoveryRequired) recovery.value = await protectionAPI.frontingRecovery(operation.value.operationId)
	}
})

const invalidatePlan = () => { plan.value = undefined }
const addRoute = () => { draft.routes.push({ sni: '', targetOptionId: '' }); invalidatePlan() }
const removeRoute = (index: number) => { draft.routes.splice(index, 1); invalidatePlan() }

const preview = () => run(async () => {
	if (!previewRequest.value) { errorCode.value = 'selector_invalid'; return }
	plan.value = await protectionAPI.frontingPreview(previewRequest.value)
})

const openDialog = (action: 'prepare' | 'apply' | 'rollback') => {
	dialogAction.value = action
	confirmation.value = ''
	idempotencyKey.value = globalThis.crypto?.randomUUID?.() ?? `fronting-${Date.now()}-${Math.random().toString(16).slice(2)}`
	dialog.value = true
}

const confirmMutation = () => run(async () => {
	if (confirmation.value !== expectedConfirmation.value) { errorCode.value = 'confirmation_mismatch'; return }
	if (dialogAction.value === 'prepare' && plan.value) {
		operation.value = await protectionAPI.frontingPrepare({
			planId: plan.value.planId, planDigest: plan.value.canonicalPlanDigest, resourceId: plan.value.publicSocket.resourceId,
			runtimeIdentityRevision: plan.value.runtime.identityRevision, strategyCapabilityRevision: plan.value.strategyCapabilityRevision,
			socketClaimRevision: plan.value.publicSocket.claimRevision, selectorSetRevision: plan.value.selectors.selectorSetRevision,
			targetReferenceRevisions: [...plan.value.targets.referenceRevisions], idempotencyKey: idempotencyKey.value,
			experimentalRiskAcknowledged: true, acknowledgement: confirmation.value,
		})
	} else if (dialogAction.value === 'apply' && operation.value) {
		operation.value = await protectionAPI.frontingApply({
			operationId: operation.value.operationId, operationRevision: operation.value.operationRevision,
			planDigest: operation.value.planDigest, targetAuthorityRevisions: operation.value.leases.map(lease => lease.authorityRevision),
			idempotencyKey: idempotencyKey.value, confirmation: confirmation.value,
		})
	} else if (dialogAction.value === 'rollback' && operation.value) {
		operation.value = await protectionAPI.frontingRollback({
			operationId: operation.value.operationId, operationRevision: operation.value.operationRevision,
			idempotencyKey: idempotencyKey.value, confirmation: confirmation.value,
		})
	}
	dialog.value = false
	page.value = await protectionAPI.frontingStatus()
	if (operation.value?.recoveryRequired) recovery.value = await protectionAPI.frontingRecovery(operation.value.operationId)
})

const knownReasons = new Set([
	'nginx_not_installed', 'nginx_external_managed', 'nginx_identity_unknown', 'stream_unavailable', 'ssl_preread_unavailable',
	'alpn_routing_unsupported', 'validation_unavailable', 'reload_unavailable', 'runtime_identity_stale', 'capability_stale',
	'socket_claim_stale', 'topology_mutation_blocked', 'target_reference_stale', 'target_management_forbidden', 'lease_conflict',
	'lease_stale', 'lease_lost', 'proxy_protocol_mismatch', 'selector_invalid', 'selector_conflict', 'default_policy_invalid',
	'plan_expired', 'plan_digest_mismatch', 'operation_conflict', 'operation_revision_stale', 'apply_gate_disabled',
	'experimental_ack_required', 'confirmation_mismatch', 'validation_failed', 'reload_failed', 'active_revision_mismatch',
	'listener_identity_mismatch', 'health_failed', 'rollback_failed', 'reconcile_required', 'ambiguous_result', 'operation_not_found',
	'legacy_fronting_requires_v2_preview', 'http_terminating_not_shipped', 'udp_quic_out_of_scope',
])
const reasonLabel = (code: string) => knownReasons.has(code) ? t(`serverProtection.fronting.reasons.${code}`) : t('serverProtection.fronting.reasons.unknown')
const stateLabel = (value?: string) => t(`serverProtection.fronting.states.${normalizeFrontingState(value)}`)
const strategyLabel = (value?: string) => t(`serverProtection.fronting.strategies.${normalizeFrontingStrategy(value)}`)
const supportLabel = (value?: string) => value === 'SUPPORTED' || value === 'HEALTHY' ? t('serverProtection.fronting.supported') : value === 'UNSUPPORTED' || value === 'FAILED' ? t('serverProtection.fronting.unsupported') : t('serverProtection.fronting.unknown')
const runtimeLabel = (value?: string) => value === 'NGINX_NOT_INSTALLED' ? t('serverProtection.fronting.nginxNotInstalled') : value === 'MANAGED_ENGINE_READY' ? t('serverProtection.fronting.nginxManagedReady') : value === 'NGINX_EXTERNAL_MANAGED' ? t('serverProtection.fronting.nginxExternal') : t('serverProtection.fronting.nginxUnknown')
const installationLabel = (value?: string) => value === 'SOLOVEY_MANAGED' ? t('serverProtection.fronting.installManaged') : value === 'EXTERNAL_MANAGED' ? t('serverProtection.fronting.installExternal') : t('serverProtection.fronting.installUnknown')
const actionLabel = (value?: string) => ['PREVIEW', 'APPLY_OR_ROLLBACK', 'ROLLBACK', 'INSPECT_RECOVERY', 'REFRESH'].includes(value || '') ? t(`serverProtection.fronting.actions.${value}`) : t('serverProtection.fronting.actions.UNKNOWN')
const yesNo = (value: boolean) => value ? t('serverProtection.fronting.yes') : t('serverProtection.fronting.no')

onMounted(refresh)
</script>
