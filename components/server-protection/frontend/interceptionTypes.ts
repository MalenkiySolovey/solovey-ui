export type InterceptionDisposition =
  | 'SHIP'
  | 'INSPECTION_ONLY'
  | 'BLOCKED_MISSING_CAPABILITY'
  | 'NOT_SHIPPED'
  | 'EXTERNAL_MANAGED'

export interface InterceptionFact {
  schema: string
  providerId: string
  providerRevision: string
  resourceId: string
  endpointId: string
  inboundDatabaseId: number
  inboundTag?: string
  kind: 'REDIRECT' | 'TPROXY' | 'TUN'
  network: 'tcp' | 'udp'
  addressFamily: 'ipv4' | 'ipv6'
  configuredBind: string
  configuredPort: number
  observedEndpointId?: string
  ownership: 'PROVIDER_MANAGED' | 'EXTERNAL_MANAGED' | 'UNKNOWN'
  listenerState: string
  configurationRevision: string
  runtimeRevision: string
  runtimeGenerationRevision: string
  listenerRevision: string
  coreSemanticRevision: string
  linuxOnly: boolean
  transparentSocketRequired: boolean
  originalDestinationMechanism: string
  originalDestinationPreserved: boolean
  sourcePreserved: boolean
  policyRoutingRequired: boolean
  boundedUdpFlowState: boolean
  healthCapabilityReady: boolean
  runtimeReady: boolean
  localOutputCapture: boolean
  tunOwned: boolean
  observedAt: number
  expiresAt: number
  factRevision: string
  reasonCodes?: string[]
}

export interface IngressScopeFact {
  providerId: string
  scopeId: string
  interfaceName: string
  interfaceIndex: number
  interfaceRevision: string
  addressFamily: 'ipv4' | 'ipv6'
  ownership: string
  forwardedIngress: boolean
  loopback: boolean
  virtual: boolean
  management: boolean
  externalManaged: boolean
  expiresAt: number
  scopeRevision: string
  reasonCodes?: string[]
}

export interface InterceptionReference {
  schema: string
  providerId: string
  resourceId: string
  endpointId: string
  kind: InterceptionFact['kind']
  network: InterceptionFact['network']
  addressFamily: InterceptionFact['addressFamily']
  factRevision: string
  configurationRevision: string
  runtimeRevision: string
  listenerRevision: string
  canonicalReferenceRevision: string
}

export interface InterceptionResourceStatus {
  fact: InterceptionFact
  reference: InterceptionReference
  disposition: InterceptionDisposition
  reasonCodes: string[]
}

export interface InterceptionStatus {
  schema: string
  generatedAt: number
  architectureRevision: string
  experimental: boolean
  defaultEnabled: boolean
  mutationAvailable: boolean
  forwardedIngressOnly: boolean
  localOutputShipped: boolean
  tunAdoptionShipped: boolean
  allocatorState: string
  helperState: string
  healthState: string
  resources: InterceptionResourceStatus[]
  ingressScopes: IngressScopeFact[]
  profileMatrix: Array<{
    kind: InterceptionFact['kind']
    network: InterceptionFact['network']
    addressFamily: InterceptionFact['addressFamily']
    disposition: InterceptionDisposition
    reasonCodes: string[]
  }>
  globalReasonCodes: string[]
}

export interface InterceptionPlan {
  schema: string
  planId: string
  planRevision: string
  generatedAt: number
  expiresAt: number
  interception: InterceptionReference
  fact: InterceptionFact
  eligibleIngressScopes: unknown[]
  disposition: InterceptionDisposition
  desiredState: string
  selectedState: string
  actualState: string
  allocatorState: string
  managedMark: number | null
  managedMask: number | null
  routingTable: number | null
  rulePriority: number | null
  reasonCodes: string[]
}
