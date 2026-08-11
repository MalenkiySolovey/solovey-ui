export type LocalProxyProtocol = 'SOCKS4' | 'SOCKS5' | 'HTTP_FORWARD' | 'HTTP_CONNECT'
export type LocalProxyActualState =
	| 'NOT_APPLIED' | 'PREPARED' | 'APPLYING' | 'HEALTH' | 'APPLIED_EXPERIMENTAL'
	| 'DEGRADED' | 'BLOCKED' | 'ROLLING_BACK' | 'RECOVERY_REQUIRED'
	| 'EXTERNAL_MANAGED' | 'UNSUPPORTED' | 'UNKNOWN'

export interface LocalProxyFact {
	schema: string
	providerId: string
	contributorId: string
	resourceId: string
	endpointId: string
	inboundDatabaseId: number
	inboundType: 'socks' | 'http' | 'mixed' | string
	configurationRevision: string
	effectiveRuntimeRevision: string
	runtimeIdentityRevision: string
	providerRevision: string
	capabilityRevision: string
	listenerObservationRevision?: string
	ownerRevision: string
	healthRevision: string
	capacityRevision: string
	factRevision: string
	configuredBind: string
	configuredPort: number
	addressFamily: string
	observedBind?: string
	observedPort?: number
	observedAddressFamily?: string
	exposure: 'LOOPBACK' | 'PRIVATE' | 'PUBLIC' | 'WILDCARD' | 'UNSPECIFIED' | 'UNKNOWN'
	ownership: 'PROVIDER_MANAGED' | 'EXTERNAL_MANAGED' | 'UNKNOWN'
	listenerState: 'OBSERVED_EXACT' | 'UNOBSERVED' | 'STALE' | 'FOREIGN' | 'UNKNOWN'
	protocols: LocalProxyProtocol[]
	authentication: 'PRESENT' | 'ABSENT' | 'UNKNOWN'
	authenticationCount: number
	authenticationRevision: string
	tls: 'ENABLED' | 'DISABLED' | 'UNKNOWN'
	tlsRevision: string
	systemProxy: 'ENABLED' | 'DISABLED' | 'UNKNOWN'
	systemProxyRevision: string
	dependentUdpAssociation: boolean
	staticUdpListener: boolean
	runtimeReady: boolean
	healthCapabilityReady: boolean
	capacityReady: boolean
	managementCollision: string
	recoveryPathCollision: string
	observedAt: number
	expiresAt: number
	reasonCodes?: string[]
}

export interface LocalProxyPlan {
	schema: string
	planId: string
	planDigest: string
	createdAt: number
	expiresAt: number
	resourceId: string
	endpointId: string
	factRevision: string
	fact: LocalProxyFact
	actualState: LocalProxyActualState
	applyGate: 'EXPERIMENTAL_ACK_REQUIRED' | 'BLOCKED'
	blockCodes?: string[]
	warningCodes?: string[]
}

export interface LocalProxyLease {
	leaseId: string
	revision: string
	state: 'RESERVED' | 'MUTATION_PENDING' | 'ACTIVE' | 'RECONCILE_REQUIRED' | 'RELEASED' | string
	renewedAt: number
	expiresAt: number
}

export interface LocalProxyHealth {
	protocol: LocalProxyProtocol
	passed: boolean
	positiveTransaction: boolean
	missingAuthenticationDenied: boolean
	invalidAuthenticationDenied: boolean
	exactTarget: boolean
	exactSink: boolean
	generation: number
	completedUnixNano: number
	expiresUnixNano: number
	revision: string
}

export interface LocalProxyState {
	resourceId: string
	endpointId: string
	actualState: LocalProxyActualState
	applyGate: string
	planId: string
	planDigest: string
	factRevision: string
	lease: LocalProxyLease
	latestOperationId: string
	latestOperationRevision: number
	markerRevision?: string
	health: LocalProxyHealth[]
	healthRevision?: string
	healthExpiresUnixNano?: number
	providerGuarded: boolean
	recoveryRequired: boolean
	updatedAt: number
}

export interface LocalProxyStatus {
	schema: string
	generatedAt: number
	facts: LocalProxyFact[]
	plans: LocalProxyPlan[]
	states: LocalProxyState[]
	experimental: true
	defaultApplyEnabled: false
	reasonCodes?: string[]
}

export interface LocalProxyPrepareRequest {
	resourceId: string
	endpointId: string
	factRevision: string
	planId: string
	planDigest: string
	idempotencyKey: string
	acknowledged: true
	confirmation: string
}

export interface LocalProxyApplyRequest {
	operationId: string
	operationRevision: number
	planId: string
	planDigest: string
	factRevision: string
	idempotencyKey: string
	acknowledged: true
	confirmation: string
}

export interface LocalProxyDisableRequest {
	operationId: string
	operationRevision: number
	idempotencyKey: string
	confirmation: string
}

export interface LocalProxyResult {
	operationId: string
	operationRevision: number
	operationState: string
	planId: string
	planDigest: string
	actualState: LocalProxyActualState
	lease: LocalProxyLease
	health?: LocalProxyHealth[]
	replayed: boolean
	recoveryRequired: boolean
	warningCodes?: string[]
}
