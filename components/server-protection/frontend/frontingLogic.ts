import type {
	FrontingActualState,
	FrontingBackendReference,
	FrontingDefaultPolicy,
	FrontingPreviewRequest,
	FrontingProxyMode,
	FrontingStatus,
	FrontingStrategy,
	FrontingTargetReference,
} from './types'

const states = new Set<FrontingActualState>([
	'NOT_APPLIED', 'PREPARED', 'APPLYING', 'HEALTH', 'APPLIED', 'DEGRADED', 'ROLLING_BACK', 'ROLLED_BACK',
	'ROLLBACK_FAILED', 'RECONCILE_REQUIRED', 'CANCELLED', 'UNKNOWN',
])

const strategies = new Set<FrontingStrategy>([
	'L4_ONE_TO_ONE_FRONTING', 'SNI_PREREAD_FRONTING', 'HTTP_TERMINATING_FRONTING', 'UDP_QUIC', 'UNKNOWN',
])

export const normalizeFrontingState = (value?: string): FrontingActualState =>
	value && states.has(value as FrontingActualState) ? value as FrontingActualState : 'UNKNOWN'

export const normalizeFrontingStrategy = (value?: string): FrontingStrategy =>
	value && strategies.has(value as FrontingStrategy) ? value as FrontingStrategy : 'UNKNOWN'

export const validExactSNI = (value: string): boolean => {
	if (!value || value.length > 253 || value !== value.toLowerCase() || value.includes('*') || /[^\x21-\x7e]/.test(value)) return false
	const labels = value.split('.')
	return labels.length >= 2 && labels.every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))
}

export interface FrontingTargetOption {
	id: string
	label: string
	referenceRevision: string
	backend?: FrontingBackendReference
	fallback?: FrontingTargetReference
}

export const targetOptions = (status?: FrontingStatus): FrontingTargetOption[] => {
	if (!status) return []
	const backends = status.backendReferences.map(reference => ({
		id: `backend:${reference.canonicalReferenceRevision}`,
		label: `${reference.providerId} · ${reference.resourceId} · ${reference.endpointId}`,
		referenceRevision: reference.canonicalReferenceRevision,
		backend: reference,
	}))
	const fallbacks = status.fallbackReferences.map(summary => ({
		id: `fallback:${summary.referenceRevision}`,
		label: `${summary.reference.providerId} · ${summary.reference.targetId} · ${summary.reference.endpointId}`,
		referenceRevision: summary.referenceRevision,
		fallback: summary.reference,
	}))
	return [...backends, ...fallbacks]
}

export interface FrontingEditorDraft {
	resourceId: string
	strategy: 'L4_ONE_TO_ONE_FRONTING' | 'SNI_PREREAD_FRONTING'
	socketClaimRevision: string
	proxyMode: FrontingProxyMode
	routes: Array<{ sni: string; targetOptionId: string }>
	defaultPolicy: FrontingDefaultPolicy
	defaultTargetOptionId: string
}

export const buildFrontingPreviewRequest = (status: FrontingStatus, draft: FrontingEditorDraft): FrontingPreviewRequest | undefined => {
	const socket = status.socketClaims.find(claim => claim.claimRevision === draft.socketClaimRevision)
	if (!socket || socket.resourceId !== status.resourceId || !socket.topologyMutationEligible) return undefined
	const options = targetOptions(status)
	const selected = new Map<string, FrontingTargetOption>()
	if (draft.strategy === 'L4_ONE_TO_ONE_FRONTING') {
		const option = options.find(item => item.id === draft.defaultTargetOptionId)
		if (!option) return undefined
		selected.set(option.id, option)
	} else {
		for (const route of draft.routes) {
			const option = options.find(item => item.id === route.targetOptionId)
			if (!option || !validExactSNI(route.sni)) return undefined
			selected.set(option.id, option)
		}
		if (!draft.routes.length) return undefined
		if (draft.defaultPolicy !== 'REJECT') {
			const option = options.find(item => item.id === draft.defaultTargetOptionId)
			if (!option) return undefined
			selected.set(option.id, option)
		}
	}
	return {
		resourceId: status.resourceId,
		expectedCurrentConfigurationRevision: socket.currentConfigurationRevision,
		requestedStrategy: draft.strategy,
		socketClaim: { resourceId: socket.resourceId, endpointId: socket.endpointId, claimRevision: socket.claimRevision },
		backendReferences: [...selected.values()].flatMap(option => option.backend ? [option.backend] : []),
		fallbackReferences: [...selected.values()].flatMap(option => option.fallback ? [option.fallback] : []),
		selectedProxyMode: draft.proxyMode,
		selectors: draft.strategy === 'SNI_PREREAD_FRONTING' ? draft.routes.map(route => ({
			sni: route.sni,
			alpn: [],
			targetReferenceRevision: options.find(item => item.id === route.targetOptionId)?.referenceRevision ?? '',
		})) : [],
		default: {
			policy: draft.strategy === 'L4_ONE_TO_ONE_FRONTING' ? 'REJECT' : draft.defaultPolicy,
			targetReferenceRevision: draft.strategy === 'SNI_PREREAD_FRONTING' && draft.defaultPolicy !== 'REJECT'
				? options.find(item => item.id === draft.defaultTargetOptionId)?.referenceRevision
				: undefined,
		},
	}
}

export const canPrepareFronting = (planExists: boolean, blocks: string[]): boolean => planExists && blocks.length === 0
export const canApplyFronting = (state?: string): boolean => normalizeFrontingState(state) === 'PREPARED'
export const canRollbackFronting = (state?: string): boolean => ['APPLIED', 'DEGRADED', 'RECONCILE_REQUIRED'].includes(normalizeFrontingState(state))
