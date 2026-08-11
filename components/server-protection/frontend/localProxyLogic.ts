import type { LocalProxyPlan, LocalProxyState } from './localProxyTypes'

export const localProxyPlanReference = (plan: LocalProxyPlan) => ({
	resourceId: plan.resourceId,
	endpointId: plan.endpointId,
	factRevision: plan.factRevision,
})

export const localProxyStateFor = (plan: LocalProxyPlan, states: LocalProxyState[]): LocalProxyState | undefined =>
	states.find(state => state.resourceId === plan.resourceId && state.endpointId === plan.endpointId)

export const localProxyCanPrepare = (plan: LocalProxyPlan, state?: LocalProxyState): boolean =>
	(!state || state.actualState === 'NOT_APPLIED') &&
	plan.applyGate === 'EXPERIMENTAL_ACK_REQUIRED' && (plan.blockCodes?.length ?? 0) === 0

export const localProxyCanApply = (state?: LocalProxyState): boolean =>
	state?.actualState === 'PREPARED' && !!state.latestOperationId && state.latestOperationRevision > 0 &&
	!state.recoveryRequired && state.lease.state === 'RESERVED'

export const localProxyCanDisable = (state?: LocalProxyState): boolean =>
	!!state && ['APPLIED_EXPERIMENTAL', 'DEGRADED'].includes(state.actualState) &&
	!!state.latestOperationId && state.latestOperationRevision > 0 && state.lease.state === 'ACTIVE'

export const localProxyHealthComplete = (plan: LocalProxyPlan, state?: LocalProxyState): boolean => {
	if (!state || state.actualState !== 'APPLIED_EXPERIMENTAL') return false
	const passed = new Set((state.health ?? []).filter(item => item.passed && item.exactTarget && item.exactSink).map(item => item.protocol))
	return plan.fact.protocols.every(protocol => passed.has(protocol))
}
