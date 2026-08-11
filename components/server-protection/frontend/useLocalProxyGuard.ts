import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { protectionAPI } from './api'
import {
	localProxyCanApply, localProxyCanDisable, localProxyCanPrepare, localProxyPlanReference, localProxyStateFor,
} from './localProxyLogic'
import type { LocalProxyPlan, LocalProxyState, LocalProxyStatus } from './localProxyTypes'

type LocalProxyAction = 'prepare' | 'apply' | 'disable'

const message = (reason: unknown): string => reason instanceof Error ? reason.message : String(reason)
const idempotency = () => globalThis.crypto?.randomUUID?.() ?? `local-proxy-${Date.now()}`

export const useLocalProxyGuard = () => {
	const { t } = useI18n()
	const status = ref<LocalProxyStatus>()
	const selected = ref<LocalProxyPlan>()
	const action = ref<LocalProxyAction>('prepare')
	const loading = ref(false)
	const error = ref('')
	const confirmOpen = ref(false)
	const acknowledged = ref(false)
	const confirmation = ref('')

	const stateFor = (plan: LocalProxyPlan): LocalProxyState | undefined =>
		localProxyStateFor(plan, status.value?.states ?? [])

	const expectedConfirmation = computed(() => {
		if (!selected.value) return ''
		if (action.value === 'prepare') return `PREPARE LOCAL PROXY ${selected.value.planId}`
		const state = stateFor(selected.value)
		return `${action.value === 'apply' ? 'APPLY' : 'DISABLE'} LOCAL PROXY ${state?.latestOperationId ?? ''}`
	})

	const load = async (refresh = false) => {
		loading.value = true
		error.value = ''
		try {
			status.value = await protectionAPI.localProxyStatus(refresh)
		} catch (reason) {
			error.value = message(reason)
		} finally {
			loading.value = false
		}
	}

	const preview = async (plan: LocalProxyPlan) => {
		loading.value = true
		error.value = ''
		try {
			selected.value = await protectionAPI.localProxyPreview(localProxyPlanReference(plan))
		} catch (reason) {
			error.value = message(reason)
		} finally {
			loading.value = false
		}
	}

	const openAction = (next: LocalProxyAction, plan: LocalProxyPlan) => {
		const state = stateFor(plan)
		if (next === 'prepare' && !localProxyCanPrepare(plan, state)) return
		if (next === 'apply' && !localProxyCanApply(state)) return
		if (next === 'disable' && !localProxyCanDisable(state)) return
		selected.value = plan
		action.value = next
		acknowledged.value = false
		confirmation.value = ''
		confirmOpen.value = true
	}

	const confirmAction = async () => {
		const plan = selected.value
		if (!plan) return
		const state = stateFor(plan)
		loading.value = true
		error.value = ''
		try {
			if (action.value === 'prepare') {
				await protectionAPI.localProxyPrepare({
					...localProxyPlanReference(plan), planId: plan.planId, planDigest: plan.planDigest,
					idempotencyKey: idempotency(), acknowledged: true, confirmation: confirmation.value,
				})
			} else if (action.value === 'apply' && state) {
				await protectionAPI.localProxyApply({
					operationId: state.latestOperationId, operationRevision: state.latestOperationRevision,
					planId: state.planId, planDigest: state.planDigest, factRevision: state.factRevision,
					idempotencyKey: idempotency(), acknowledged: true, confirmation: confirmation.value,
				})
			} else if (action.value === 'disable' && state) {
				await protectionAPI.localProxyDisable({
					operationId: state.latestOperationId, operationRevision: state.latestOperationRevision,
					idempotencyKey: idempotency(), confirmation: confirmation.value,
				})
			}
			confirmOpen.value = false
			await load(true)
		} catch (reason) {
			error.value = message(reason)
			loading.value = false
		}
	}

	onMounted(() => load(false))

	return {
		t, status, selected, action, loading, error, confirmOpen, acknowledged, confirmation,
		expectedConfirmation, stateFor, load, preview, openAction, confirmAction,
		localProxyCanPrepare, localProxyCanApply, localProxyCanDisable,
	}
}
