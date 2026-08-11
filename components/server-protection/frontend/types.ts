export interface ProtectionStatus {
  enabled: boolean
  revision: number
  supportState: string
  readiness: string
  blockers: string[]
	counters: { resources: number; collisions: number; profiles: number; events: number; recovery: number }
  platform: { os: string; arch: string }
}

export interface ProtectionOperation {
  operationId: string
  kind: string
  resourceId?: string
  protocol?: string
  listen?: string
  port?: number
  state: string
  revision: number
	planRevision?: string
  recoveryAttempts: number
  recoveryErrorCode?: string
	recoveryBundleAvailable: boolean
  createdAt: number
  updatedAt: number
}

export interface OperationsState {
  items: ProtectionOperation[]
  recoveryRequired: number
  confirmationTemplates: Record<string, string>
}

export interface ResourceCapabilities {
  known: boolean
  acceptsProxyProtocol: string
  supportsGracefulDrain: string
  canServeFallback: string
  requiresAcmeHttp01: string
  requiresTlsAlpn01: string
  publicHostnames?: string[]
  routeHints?: string[]
  ownerRevision?: string
  configRevision?: string
}

export interface ProtectableResource {
  id: string
  kind: string
  owner: string
  name: string
  protocol: string
  listen: string
  port: number
  public: boolean
  tls: boolean
  source: string
  fingerprint: string
  inboundTag?: string
  capabilities: ResourceCapabilities
	endpoints?: PublicEndpoint[]
  warnings?: string[]
}

export interface PublicEndpoint {
	id: string
	key: { network: 'tcp' | 'udp' | 'unknown'; addressFamily: 'ipv4' | 'ipv6' | 'unknown'; bindAddress: string; port: number }
	intent: 'public' | 'private' | 'local' | 'unknown'
	owner: string
	source: string
	confidenceBp: number
	observedAt: number
	reasonCodes?: string[]
}

export interface HostSurfaceFact {
	id: string
	network: 'tcp' | 'udp' | 'unknown'
	family: 'ipv4' | 'ipv6' | 'unknown'
	bind: string
	port: number
	exposure: 'public' | 'private' | 'local' | 'unknown'
	desiredOwner?: string
	source: string
	confidenceBp: number
	lastSeen: number
	classification: string
	stale: boolean
	truncated: boolean
	reasonCodes?: string[]
}

export interface FallbackTargetFact {
	identity: { providerId: string; targetId: string }
	publishRevision: string
	contentDigest: string
	endpoint: { endpointId: string; network: string; family: string; bind: string; port: number; local: boolean }
	readiness: string
	providerHealthRevision: string
	observedAt: number
	expiresAt: number
	reasonCodes?: string[]
}

export type NativeFallbackActual =
	| 'NOT_APPLIED' | 'PREPARED' | 'APPLYING' | 'HEALTH' | 'APPLIED' | 'DEGRADED'
	| 'ROLLING_BACK' | 'ROLLED_BACK' | 'ROLLBACK_FAILED' | 'RECONCILE_REQUIRED'
export type NativeFallbackVariant =
	| 'NONE' | 'UNSUPPORTED' | 'VLESS_REALITY_HANDSHAKE_TCP'
	| 'TROJAN_DEFAULT_FALLBACK_TCP' | 'TROJAN_ALPN_FALLBACK_TCP'
export type NativeFallbackApplyGate = 'DISABLED_BY_DEFAULT' | 'EXPERIMENTAL' | 'STABLE'

export interface FallbackTargetReferenceV2 {
	schema: string
	providerId: string
	targetId: string
	publishRevision: string
	contentDigest: string
	endpointId: string
	endpointRevision: string
	providerHealthRevision: string
	capacityRevision: string
	providerRevision: string
}

export interface NativeTargetHealth {
	state: string
	revision?: string
	observedAt?: number
	expiresAt?: number
	fresh: boolean
	reasonCodes?: string[]
}

export interface NativeTargetCapacity {
	state: string
	revision?: string
	slotsTotal: number
	slotsUsed: number
	observedAt?: number
	expiresAt?: number
	fresh: boolean
	reasonCodes?: string[]
}

export interface FallbackTargetV2Summary {
	identity: { providerId: string; targetId: string }
	reference: FallbackTargetReferenceV2
	endpointId: string
	endpointMode: 'TLS_HANDSHAKE_TARGET' | 'PLAINTEXT_POST_TLS_TARGET' | string
	transportSecurity: 'TLS' | 'PLAINTEXT' | 'UNKNOWN' | string
	applicationProtocols: string[]
	acceptedServerNameCount: number
	health: NativeTargetHealth
	capacity: NativeTargetCapacity
	providerRevision: string
	actionable: boolean
	reasonCodes?: string[]
}

export interface NativeFallbackPlan {
	schema: string
	planId: string
	planDigest: string
	createdAt: string
	expiresAt: string
	resource: {
		resourceId: string
		inboundDatabaseId: number
		inboundTag: string
		inboundType: string
		sourceRevision: string
		resourceRevision: string
		configurationRevision: string
		effectiveRevision?: string
	}
	runtime: { identityRevision: string; capabilityResolverRevision: string; admittedVariant: NativeFallbackVariant | string }
	target: {
		reference: FallbackTargetReferenceV2
		canonicalTargetRevision?: string
		endpointId?: string
		endpointRevision?: string
		publishRevision?: string
		providerRevision?: string
		healthRevision?: string
		healthState?: string
		healthExpiresAt?: string
		capacityRevision?: string
		capacityState?: string
		capacityExpiresAt?: string
		reservationSlotsTotal?: number
		reservationSlotsUsed?: number
		transportSecurity?: string
		applicationProtocols?: string[]
		managementReachability?: string
	}
	corePreview: {
		beforeConfigurationRevision?: string
		expectedAfterRevision?: string
		replaceDefaultToo: boolean
		expiresAt?: string
	}
	managementIsolation: { state: string; revision?: string; expiresAt?: string; reasonCodes?: string[] }
	applyGate: NativeFallbackApplyGate | string
	desiredState: string
	selectedVariant: NativeFallbackVariant | string
	actualState: NativeFallbackActual | string
	eligible: boolean
	blocks?: string[]
	warnings?: string[]
	reasonCodes?: string[]
}

export interface NativeFallbackOperation {
	operationId: string
	resourceId: string
	revision: number
	state: string
	planDigest: string
	providerReservationState?: string
	providerReservationRevision?: string
	actualState: NativeFallbackActual | string
	recoveryRequired: boolean
	reasonCodes?: string[]
	createdAt: number
	updatedAt: number
}

export interface NativeFallbackPreviewRequest {
	resourceId: string
	expectedConfigRevision: string
	targetReference: FallbackTargetReferenceV2
}

export interface NativeFallbackPrepareRequest {
	planId: string
	planDigest: string
	resourceId: string
	sourceRevision: string
	resourceRevision: string
	configurationRevision: string
	effectiveRevision: string
	runtimeIdentityRevision: string
	capabilityResolverRevision: string
	canonicalTargetRevision: string
	providerRevision: string
	endpointRevision: string
	publishRevision: string
	healthRevision: string
	capacityRevision: string
	targetReference: FallbackTargetReferenceV2
	idempotencyKey: string
	experimentalRiskAcknowledged: true
}

export interface NativeFallbackApplyRequest {
	operationId: string
	operationRevision: number
	planDigest: string
	providerReservationRevision: string
	idempotencyKey: string
	confirmation: string
}

export type NativeFallbackRollbackRequest = NativeFallbackApplyRequest

export interface NativeFallbackStatus {
	resourceId: string
	inbound: { databaseId: number; tag: string; type: string }
	desiredState: string
	selectedVariant: NativeFallbackVariant | string
	actualState: NativeFallbackActual | string
	runtime: { status: string; revision?: string }
	capability: {
		status: string
		revision?: string
		variant: NativeFallbackVariant | string
		naturalInvalidTrafficFallback: boolean
		forcedSameSubjectDecoyRoute: boolean
	}
	configurationRevision?: string
	effectiveRevision?: string
	target?: FallbackTargetV2Summary
	providerReservation?: { state: string; revision?: string; expiresAt?: number; fresh: boolean }
	latestOperation?: NativeFallbackOperation
	recoveryStatus: string
	applyGate: NativeFallbackApplyGate | string
	blocks?: string[]
	warnings?: string[]
	reasonCodes?: string[]
	updatedAt?: number
}

export interface NativeFallbackStatusPage {
	items: NativeFallbackStatus[]
	page: number
	limit: number
	total: number
	generatedAt: number
}

export interface NativeFallbackStatusQuery {
	resource_id?: string
	page?: number
	limit?: number
}

export interface NativeTargetInspection {
	items: Array<{
		identity: { providerId: string; targetId: string }
		endpointId: string
		readiness: string
		observedAt: number
		expiresAt: number
		reasonCodes?: string[]
		legacy: true
		actionable: false
	}>
	targetsV2: FallbackTargetV2Summary[]
	page: number
	limit: number
	total: number
	totalV2: number
	generatedAt: number
	reasonCodes?: string[]
	reservations: Array<{ providerId: string; targetId: string; state: string; revision: string; expiresAt: number; fresh: boolean }>
	reservationsTruncated: boolean
}

export interface SignalV2 {
	signalId: string
	category: string
	kind: string
	knownKind: boolean
	scope: { scope: string; targetResourceId?: string }
	source: { sourceId: string; sourceClass: string }
	observedAt: string
	expiresAt: string
	confidenceBp: number
	reasonCodes?: string[]
}

export interface DecisionV2 {
	decisionId: string
	scope: { scope: string; targetResourceId?: string }
	requestedIntent: string
	state: string
	capabilityResolution: { implemented: boolean; resolvedIntent: string; reasonCodes?: string[] }
	createdAt: string
	expiresAt: string
	reasonCodes?: string[]
}

export interface PostureFacts {
	managementEndpoints: Array<{ id: string; serviceKind: string; network: string; family: string; bind: string; port: number; exposure: string; owner: string; source: string; confidenceBp: number; reasonCodes?: string[] }>
	recoveryPaths: Array<{ id: string; kind: string; endpointId: string; verificationMethod: string; verifiedAt: number; expiresAt: number; independenceClass: string; verificationState: string; sourceRevision: string; configurationRevision: string; reasonCodes?: string[] }>
	recoveryState: string
	capabilities: Array<{ kind: string; state: string }>
	implemented: string
	planned: string
}

export interface FirewallBaselineClaim {
	id: string
	kind: 'desired' | 'observed'
	key: { network: string; addressFamily: string; bindAddress: string; port: number }
	owner?: string
	ownerRevision?: string
	configurationRevision?: string
	stale: boolean
	truncated: boolean
	ambiguous: boolean
	reasonCodes?: string[]
}

export interface FirewallBaselineGraphNode {
	resourceId: string
	resourceOwner: string
	ownerRevision: string
	configurationRevision: string
	desiredClaims: FirewallBaselineClaim[]
	observedClaims: FirewallBaselineClaim[]
	selectedStrategy: string
	applyBlocked: boolean
	reasonCodes?: string[]
	alternatives: Array<{ code: string; resourceId?: string; strategy?: string; detail: string }>
}

export interface FirewallBaselineSnapshot {
	recommendations: Array<{ resourceId: string; strategy: string; reasonCodes?: string[]; applyBlocked: boolean; ownerRevision: string; configurationRevision: string }>
	socketGraph: { revision: string; generatedAt: number; nodes: FirewallBaselineGraphNode[]; collisions: Array<{ code: string; leftResourceId: string; rightResourceId: string; alternatives: Array<{ code: string; detail: string }> }>; applyBlocked: boolean; reasonCodes?: string[] }
	kernelPlan: { revision: string; inputRevision: string; graphRevision: string; mode: string; applyBlocked: boolean; reasonCodes?: string[]; endpoints: Array<{ endpointRevision: string; resourceId: string; owner: string; ownerRevision: string; configurationRevision: string; strategy: string; desiredStatus: string; selectedStatus: string; actualStatus: string; key: { network: string; addressFamily: string; bindAddress: string; port: number } }> }
	firewallBaselineEligibility: { kind: string; revision: string; candidateEligible: boolean; mutationReady: boolean; endpointInventoryComplete: boolean; managementPreserved: boolean; exactRevisions: boolean; managedTableOnly: boolean; noForeignMutation: boolean; reasonCodes?: string[]; mutationReasonCodes?: string[]; advisoryCodes?: string[] }
	listenerTopologyMutationEligibility: { kind: string; revision: string; eligible: boolean; graphRevision: string; ownerObservationRevision?: string; reasonCodes?: string[] }
	kernelPreview: FirewallPreview
	snapshotBinding: { schema: string; revision: string; runtimeRevision: string; resourceRevision: string; graphRevision: string; configurationRevision: string; policyRevision: string; recoveryRevision: string; planRevision: string; candidateSha256: string; capturedAt: number }
	capabilityAssessment: { capabilityRevision?: string; ttlRequired: boolean; ttlSupported: boolean; rateRequired: boolean; rateSupported: boolean; candidateSupported: boolean; advancedState: string; acceptanceConsequence: string; sshRecoverySupported: boolean; sshVerifierRevision?: string }
	managementGuard: { state: string; invalidRecoveryRecords: number; recoveryPaths?: PostureFacts['recoveryPaths'] }
	status: { desired: string; selected: string; actual: string }
	realNftablesLive: 'NOT_RUN'
	stabilityClaim: string
}

export interface Collision {
  code: string
  severity: string
  leftResourceId: string
  rightResourceId: string
  protocol: string
  port: number
  message: string
}

export interface Inventory {
  generatedAt: number
  resources: ProtectableResource[]
  collisions?: Collision[]
  warnings?: Array<{ owner: string; resourceId?: string; code: string; message: string }>
  errors?: Array<{ owner: string; message: string }>
}

export interface Profile {
  id: number
  resourceId: string
  resourceKind: string
  resourceOwner: string
  enabled: boolean
  status: string
  mode: string
  acceptedFingerprint: string
  lastSeenFingerprint: string
  resourceFingerprint?: string
  scoreThreshold: number
  graylistTtlSeconds: number
  defaultAction: string
  revision: number
}

export interface ProbeEvent {
  id: number
  resourceId: string
  resourceKind: string
  sourceIpCidr?: string
  signalKind: string
  scoreDelta: number
  action: string
  safeMeta?: Record<string, unknown>
  observedAt: number
}

export interface GraylistEntry {
  id: number
  resourceId: string
  ipCidr: string
  ipFamily: number
  score: number
  reason: string
  lastSignal: string
  expiresAt: number
  updatedAt: number
}

export interface ProtectionSettings {
  enabled: boolean
  retentionGlobalLimit: number
  retentionPerResourceLimit: number
  defaultScoreThreshold: number
  defaultGraylistTtlSeconds: number
  diagnosticsCacheTtlSeconds: number
  observationBufferSize: number
  observationFlushIntervalMs: number
  ipv6GraylistPrefixBits: number
  maxScore: number
  safeMetaMaxBytes: number
  clockSkewToleranceSeconds: number
  artifactRetentionCount: number
  artifactRetentionDays: number
  advancedAcknowledgedAt?: number
  featureFlags: Record<string, boolean>
}

export interface Diagnostics {
  supportState: string
  checks: Array<{ id: string; status: string; details: unknown }>
  warnings: string[]
  platform: { os: string; arch: string }
}

export interface FirewallPreview {
  revision: string
	inputRevision?: string
  backend: string
  wouldKeep: string[]
  wouldOpen: string[]
  wouldWarn: string[]
  wouldBlock: string[]
  generatedNft?: string
  warnings: string[]
}

export interface FirewallWorkflowResult {
  operationId: string
  state: string
  revision: number
  artifactRevision?: string
  rollbackAttempted: boolean
	candidateSha256?: string
	rollbackSha256?: string
	planRevision?: string
	graphRevision?: string
	desiredStatus?: string
	selectedStatus?: string
	actualStatus?: string
	reasonCodes?: string[]
}

export type FrontingStrategy = 'L4_ONE_TO_ONE_FRONTING' | 'SNI_PREREAD_FRONTING' | 'HTTP_TERMINATING_FRONTING' | 'UDP_QUIC' | 'UNKNOWN'
export type FrontingActualState = 'NOT_APPLIED' | 'PREPARED' | 'APPLYING' | 'HEALTH' | 'APPLIED' | 'DEGRADED' | 'ROLLING_BACK' | 'ROLLED_BACK' | 'ROLLBACK_FAILED' | 'RECONCILE_REQUIRED' | 'CANCELLED' | 'UNKNOWN'
export type FrontingSupport = 'SUPPORTED' | 'UNSUPPORTED' | 'UNKNOWN'
export type FrontingProxyMode = 'OFF' | 'ON'
export type FrontingDefaultPolicy = 'REJECT' | 'FIXED_SAFE_DEFAULT' | 'NON_TLS_FIXED_TARGET'

export interface NginxModuleCapability {
	state: string
	effective: FrontingSupport | string
	revision: string
	reasonCode?: string
}

export interface NginxMethodCapability {
	availability: FrontingSupport | string
	revision: string
}

export interface NginxRuntimeStatus {
	state: string
	states: string[]
	version?: string
	installationClass: string
	canonicalRuntimeIdentityRevision: string
	stream: NginxModuleCapability
	sslPreread: NginxModuleCapability
	validationMethod: NginxMethodCapability
	reloadMethod: NginxMethodCapability
	activeVerification: NginxMethodCapability
	processVerification: NginxMethodCapability
	listenerVerification: NginxMethodCapability
	proxyProtocolReceive: NginxMethodCapability
	proxyProtocolEmit: NginxMethodCapability
	observedAt: number
	expiresAt: number
	reasonCodes?: string[]
}

export interface NginxStrategyCapability {
	strategy: FrontingStrategy | string
	capabilityRevision: string
	support: FrontingSupport | string
	actionable: boolean
	inspectionOnly: boolean
	selectedProxyMode: FrontingProxyMode | string
	proxyProtocolReceive: NginxMethodCapability
	proxyProtocolEmit: NginxMethodCapability
	blocks: string[]
	warnings: string[]
	reasonCodes: string[]
	observedAt: number
	expiresAt: number
}

export interface FrontingBackendReference {
	schema: string
	providerId: string
	contributorId: string
	resourceId: string
	endpointId: string
	endpointRevision: string
	ownerRevision: string
	providerRevision: string
	healthRevision: string
	capacityRevision?: string
	backendClass: string
	resourceKind: string
	ownership: string
	network: string
	addressFamily: string
	classification: string
	selectedTransport: string
	selectedProxyMode: FrontingProxyMode
	managementReachability: string
	canonicalReferenceRevision: string
}

export interface FrontingTargetReference {
	schema: string
	providerId: string
	targetId: string
	publishRevision: string
	contentDigest: string
	endpointId: string
	endpointRevision: string
	providerHealthRevision: string
	capacityRevision: string
	providerRevision: string
}

export interface FrontingSocketClaim {
	schema: string
	resourceId: string
	endpointId: string
	addressFamily: string
	canonicalBind: string
	wildcard: boolean
	protocol: string
	publicPort: number
	currentConfigurationRevision: string
	topologyOwnershipEligibilityRevision: string
	listenerSocketFactRevision: string
	managementExclusionRevision: string
	topologyMutationEligible: boolean
	claimRevision: string
	observedAt: number
	expiresAt: number
	reasonCodes?: string[]
}

export interface FrontingSelectorTuple {
	sni: string
	alpn?: string
	selectorId: string
	upstreamId: string
	targetReferenceRevision: string
}

export interface FrontingSelectorSet {
	schema: string
	alpnSemantics: string
	tuples: FrontingSelectorTuple[]
	targetRevisions: string[]
	default: { policy: FrontingDefaultPolicy; targetReferenceRevision?: string }
	selectorSetRevision: string
}

export interface FrontingPlan {
	schema: string
	planId: string
	canonicalPlanDigest: string
	createdAt: number
	expiresAt: number
	strategy: { desired: FrontingStrategy | string; selected?: FrontingStrategy | string; actual: FrontingActualState | string }
	applyGate: string
	strategyCapabilityRevision: string
	runtime: {
		identityRevision: string
		state: string
		streamCapabilityRevision: string
		sslPrereadCapabilityRevision: string
		moduleCapabilityRevision: string
		validationCapabilityRevision: string
		reloadCapabilityRevision: string
		verificationCapabilityRevision: string
	}
	publicSocket: FrontingSocketClaim
	targets: {
		backendReferences: FrontingBackendReference[]
		fallbackReferences: FrontingTargetReference[]
		selectedProxyMode: FrontingProxyMode
		referenceRevisions: string[]
	}
	selectors: FrontingSelectorSet
	safety: {
		managementExclusionRevision: string
		projection: { desired: FrontingStrategy | string; selected?: FrontingStrategy | string; actual: FrontingActualState | string }
		blocks: string[]
		warnings: string[]
		reasonCodes: string[]
		inputExpiries: Array<{ kind: string; revision: string; expiresAt: number }>
	}
}

export interface FrontingLeaseSummary {
	kind: string
	referenceRevision: string
	authorityRevision: string
	state: string
	expiresAt?: number
}

export interface FrontingOperation {
	operationId: string
	operationRevision: number
	resourceId: string
	strategy: FrontingStrategy | string
	workflowState: string
	actualState: FrontingActualState | string
	planDigest: string
	candidateRevision?: string
	activeRevision?: string
	socketClaimRevision?: string
	backendReferenceRevisions?: string[]
	selectorSetRevision?: string
	mapRevision?: string
	leases: FrontingLeaseSummary[]
	healthState: string
	healthObservedAt?: number
	healthExpiresAt?: number
	rollbackCount: number
	recoveryClassification?: string
	recoveryRequired: boolean
	compatibilityState: string
	reasonCodes: string[]
	safeNextAction: string
}

export interface FrontingRecoveryStatus {
	operationId: string
	operationRevision: number
	classification: string
	recoveryRequired: boolean
	checkpointRetained: boolean
	authoritiesRetained: boolean
	permittedNextAction: string
	reasonCodes: string[]
}

export interface FrontingStatus {
	schema: string
	resourceId: string
	displayIdentity: string
	runtime: NginxRuntimeStatus
	capabilities: NginxStrategyCapability[]
	desiredStrategy: string
	selectedStrategy: string
	actualState: FrontingActualState | string
	applyGate: string
	socketClaims: FrontingSocketClaim[]
	backendReferences: FrontingBackendReference[]
	fallbackReferences: Array<{ reference: FrontingTargetReference; referenceRevision: string }>
	selectorSetRevision?: string
	activeMapRevision?: string
	defaultPolicy?: string
	selectedProxyMode?: string
	leases: FrontingLeaseSummary[]
	healthState: string
	healthObservedAt?: number
	healthExpiresAt?: number
	latestOperation?: FrontingOperation
	recoveryState: string
	compatibilityState: string
	blocks: string[]
	warnings: string[]
	reasonCodes: string[]
	safeNextAction: string
	updatedAt?: number
}

export interface FrontingStatusPage { items: FrontingStatus[]; generatedAt: number }

export interface FrontingPreviewRequest {
	resourceId: string
	expectedCurrentConfigurationRevision: string
	requestedStrategy: 'L4_ONE_TO_ONE_FRONTING' | 'SNI_PREREAD_FRONTING'
	socketClaim: { resourceId: string; endpointId: string; claimRevision: string }
	backendReferences: FrontingBackendReference[]
	fallbackReferences: FrontingTargetReference[]
	selectedProxyMode: FrontingProxyMode
	selectors: Array<{ sni: string; alpn: string[]; targetReferenceRevision: string }>
	default: { policy: FrontingDefaultPolicy; targetReferenceRevision?: string }
}

export interface FrontingPrepareRequest {
	planId: string
	planDigest: string
	resourceId: string
	runtimeIdentityRevision: string
	strategyCapabilityRevision: string
	socketClaimRevision: string
	selectorSetRevision: string
	targetReferenceRevisions: string[]
	idempotencyKey: string
	experimentalRiskAcknowledged: boolean
	acknowledgement: string
}

export interface FrontingApplyRequest {
	operationId: string
	operationRevision: number
	planDigest: string
	targetAuthorityRevisions: string[]
	idempotencyKey: string
	confirmation: string
}

export interface FrontingRollbackRequest {
	operationId: string
	operationRevision: number
	idempotencyKey: string
	confirmation: string
}

export interface PortAllowlistEntry {
  id: number
  protocol: 'tcp' | 'udp'
  listen: string
  portStart: number
  portEnd: number
  reason: string
  expiresAt?: number
}

export interface IPAllowlistEntry {
  id: number
  ipCidr: string
  reason: string
  expiresAt?: number
}
