import type { UDPGuardCapability, UDPGuardPlan } from './udpGuardTypes'

export const udpPlanReference = (plan: UDPGuardPlan) => ({
	planId: plan.planId,
	planDigest: plan.planDigest,
	resourceId: plan.resourceId,
	endpointId: plan.endpointId,
	capabilityRevision: plan.capabilityRevision,
	claimRevision: plan.claim.claimRevision,
	healthRevision: plan.healthRevision,
	firewallBaselineRevision: plan.firewallBaselineRevision,
	strategy: plan.selectedStrategy,
	policyRevision: plan.flowPolicy.revision,
})

export const udpCanPrepare = (plan: UDPGuardPlan): boolean =>
	plan.actualState === 'NOT_APPLIED' && plan.applyGate !== 'BLOCKED' && (plan.blockCodes?.length ?? 0) === 0

export const udpCapabilityBadges = (capability: UDPGuardCapability): string[] => {
	const badges = [capability.configured ? 'CONFIGURED' : 'NOT_CONFIGURED', capability.observed ? 'OBSERVED' : 'UNOBSERVED']
	badges.push(capability.shippingStatus)
	badges.push(...capability.effectiveNetworks.map(network => network.toUpperCase()))
	if (capability.strategyClass.includes('QUIC')) badges.push('QUIC')
	if (capability.dependentAssociation) badges.push('PROXY_ASSOCIATION_ONLY')
	if (capability.strategyClass === 'UNSUPPORTED') badges.push('UNSUPPORTED')
	return badges
}
