export interface UDPGuardCapability {
	resourceId: string
	inboundType: string
	strategyClass: string
	shippingStatus: 'SHIP' | 'INSPECTION_ONLY' | 'NOT_SHIPPED'
	effectiveNetworks: Array<'tcp' | 'udp'>
	configured: boolean
	observed: boolean
	dependentAssociation: boolean
	buildFeatureState: string
	authenticationPresent: boolean
	tlsPresent: boolean
	protocolOwnedZeroRtt: boolean
	protocolOwnedMigration: boolean
	actualState: string
	applyGate: string
	reasonCodes?: string[]
}

export interface UDPGuardPlan {
	planId: string
	planDigest: string
	resourceId: string
	endpointId: string
	capabilityRevision: string
	buildFeatureRevision: string
	claim: {
		claimRevision: string
		protocol: 'udp'
		addressFamily: string
		configuredBind: string
		exposure: string
		port: number
		ownerRevision: string
	}
	strategyClass: string
	desiredPolicy: 'UDP_DIRECT_GUARDED'
	selectedStrategy: 'UDP_DIRECT_GUARDED'
	actualState: string
	applyGate: string
	firewallBaselineRevision: string
	healthRevision: string
	flowPolicy: {
		revision: string
		rateProfile: string
		cardinalityProfile: string
		conntrackPolicy: string
		icmpPolicy: string
	}
	blockCodes?: string[]
	warningCodes?: string[]
	latestOperationId?: string
	latestOperationRevision?: number
	recoveryRequired?: boolean
}

export interface UDPGuardStatus {
	schema: string
	generatedAt: number
	capabilities: UDPGuardCapability[]
	plans: UDPGuardPlan[]
	experimental: true
	defaultApplyEnabled: false
}

export interface UDPGuardOperationReference {
	operationId: string
	revision: number
}
