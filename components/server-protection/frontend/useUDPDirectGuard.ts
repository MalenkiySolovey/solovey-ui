import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { protectionAPI } from './api'
import { udpCanPrepare, udpPlanReference } from './udpGuardLogic'
import type { UDPGuardOperationReference, UDPGuardPlan, UDPGuardStatus } from './udpGuardTypes'

type UDPAction = 'prepare' | 'apply' | 'rollback'

const errorMessage = (reason: unknown): string => reason instanceof Error ? reason.message : String(reason)

export const useUDPDirectGuard = () => {
	const { t } = useI18n()
	const status = ref<UDPGuardStatus>()
	const loading = ref(false)
	const error = ref('')
	const selected = ref<UDPGuardPlan>()
	const operation = ref<UDPGuardOperationReference>()
	const action = ref<UDPAction>('prepare')
	const confirmOpen = ref(false)
	const riskAcknowledged = ref(false)
	const confirmation = ref('')

	const expectedConfirmation = computed(() => {
		if (!selected.value) return ''
		if (action.value === 'prepare') return `PREPARE UDP DIRECT GUARD ${selected.value.planId}`
		return `${action.value.toUpperCase()} UDP DIRECT GUARD ${operation.value?.operationId ?? ''}`
	})

	const yesNo = (value: boolean) => value ? t('serverProtection.udp.yes') : t('serverProtection.udp.no')

	const load = async (refresh = false) => {
		loading.value = true
		error.value = ''
		try {
			status.value = await protectionAPI.get<UDPGuardStatus>('/udp/status', { refresh })
		} catch (reason) {
			error.value = errorMessage(reason)
		} finally {
			loading.value = false
		}
	}

	const preview = async (plan: UDPGuardPlan) => {
		loading.value = true
		error.value = ''
		try {
			await protectionAPI.post('/udp/preview', udpPlanReference(plan))
		} catch (reason) {
			error.value = errorMessage(reason)
		} finally {
			loading.value = false
		}
	}

	const canResumeApply = (plan: UDPGuardPlan) =>
		plan.actualState === 'PREPARED' && !!plan.latestOperationId && !!plan.latestOperationRevision && !plan.recoveryRequired

	const canResumeRollback = (plan: UDPGuardPlan) =>
		['PREPARED', 'APPLIED_EXPERIMENTAL', 'DEGRADED', 'RECOVERY_REQUIRED'].includes(plan.actualState) &&
		!!plan.latestOperationId && !!plan.latestOperationRevision

	const openAction = (next: UDPAction, plan: UDPGuardPlan) => {
		if (next === 'prepare' && !udpCanPrepare(plan)) return
		if (next !== 'prepare') {
			if (!plan.latestOperationId || !plan.latestOperationRevision) return
			operation.value = { operationId: plan.latestOperationId, revision: plan.latestOperationRevision }
		}
		selected.value = plan
		action.value = next
		riskAcknowledged.value = false
		confirmation.value = ''
		confirmOpen.value = true
	}

	const confirmAction = async () => {
		if (!selected.value) return
		loading.value = true
		error.value = ''
		const idempotencyKey = globalThis.crypto?.randomUUID?.() ?? `udp-${Date.now()}`
		try {
			if (action.value === 'prepare') {
				const response = await protectionAPI.post<{ operation: UDPGuardOperationReference }>('/udp/prepare', {
					...udpPlanReference(selected.value), idempotencyKey,
					experimentalRiskAcknowledged: true, confirmation: confirmation.value,
				})
				operation.value = response.operation
			} else if (action.value === 'apply') {
				const response = await protectionAPI.post<{ result: UDPGuardOperationReference }>('/udp/apply', {
					...udpPlanReference(selected.value), operationId: operation.value?.operationId,
					operationRevision: operation.value?.revision, idempotencyKey,
					experimentalRiskAcknowledged: true, confirmation: confirmation.value,
				})
				operation.value = { operationId: response.result.operationId, revision: response.result.revision }
			} else {
				await protectionAPI.post('/udp/rollback', {
					operationId: operation.value?.operationId, operationRevision: operation.value?.revision,
					idempotencyKey, experimentalRiskAcknowledged: true, confirmation: confirmation.value,
				})
				operation.value = undefined
			}
			confirmOpen.value = false
			await load(true)
		} catch (reason) {
			error.value = errorMessage(reason)
			loading.value = false
		}
	}

	onMounted(() => load(false))

	return {
		t, status, loading, error, action, confirmOpen, riskAcknowledged, confirmation,
		expectedConfirmation, yesNo, load, preview, canResumeApply, canResumeRollback,
		openAction, confirmAction,
	}
}
