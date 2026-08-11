import HttpUtils, { type Msg } from '@/plugins/httputil'

export type Availability = 'AVAILABLE' | 'UNAVAILABLE' | 'UNKNOWN'
export type RootLoginPolicy = 'UNCHANGED' | 'YES' | 'NO' | 'PROHIBIT_PASSWORD'

export interface DesiredSSHPolicy {
  schema: 'solovey-ui/ssh-managed-policy/v1'
  maxAuthTries?: number
  loginGraceTimeSeconds?: number
  passwordAuthentication?: boolean
  kbdInteractiveAuthentication?: boolean
  permitRootLogin: RootLoginPolicy
  pubkeyAuthentication?: boolean
}

export interface SSHCapabilities {
  observePosture: Availability
  prepare: Availability
  stage: Availability
  validate: Availability
  reload: Availability
  reconnect: Availability
  rollback: Availability
  reasonCodes?: string[]
  revision: string
}

export interface ManagementEndpoint {
  schema: string
  id: string
  network: string
  family: string
  bind: string
  port: number
  serviceKind: string
  exposure: string
  owner: string
  configuredIntent: boolean
  observedListener: boolean
  wildcard: boolean
  dualStack: boolean
  observedAt: number
  expiresAt?: number
  semanticRevision?: string
  reasonCodes?: string[]
}

export interface RecoveryPath {
  id: string
  kind: string
  endpointId: string
  principalId: string
  verificationMethod: string
  evidenceProvider?: string
  targetOperation?: string
  verifiedAt: number
  expiresAt: number
  independenceClass: string
  verificationState: string
  operationBound: boolean
  singleUse: boolean
  reasonCodes?: string[]
}

export interface SSHPostureEnvelope {
  state: 'OBSERVED' | 'UNAVAILABLE'
  fresh: boolean
  reasonCodes?: string[]
  posture?: {
    schema: string
    binary: { implementation: string; versionClass: string; selected: boolean }
    service: { manager: string; unitId: string; state: string }
    configGraph: Array<{ id: string; parentId?: string; kind: string; order: number; depth: number; owner: string; modeClass: string; symlink: boolean }>
    authentication: Record<string, string | number | string[]>
    forwarding: Record<string, string>
    authorizedKeys: Record<string, string | number | boolean>
    hostKeys: Array<{ type: string; fingerprint: string; count: number; owner: string; modeClass: string }>
    matchContexts: Array<{ id: string; conditionClass: string; known: boolean }>
    semanticRevision: string
    observedAt: number
    expiresAt: number
    reasonCodes?: string[]
  }
}

export interface PreservationPlan {
  safe: boolean
  consoleVerified: boolean
  freshPubkeyReconnect: boolean
  watchdogAvailable: boolean
  earliestSafetyExpiry: number
  independentFailureDomains: string[]
  reasonCodes?: string[]
  revision: string
}

export interface SSHPreview {
  policy: DesiredSSHPolicy
  posture?: SSHPostureEnvelope['posture']
  capabilities: SSHCapabilities
  endpoints: ManagementEndpoint[]
  recoveryPaths: RecoveryPath[]
  preservation: PreservationPlan
  candidateDigest?: string
  providerRevision?: string
  postureRevision?: string
  endpointRevision: string
  recoveryRevision: string
  possible: boolean
  reasonCodes?: string[]
  revision: string
}

export interface SSHCandidate {
  operationId: string
  state: string
  revision: number
  policy: DesiredSSHPolicy
  preservation: PreservationPlan
  candidateDigest: string
  reconnectExpiresAt?: number
  rollbackAttempts: number
  restoredUntrusted: boolean
  reasonCodes?: string[]
  createdAt: number
  updatedAt: number
}

export interface SSHReconnectState {
  operationId: string
  state: string
  revision: number
  required: boolean
  expiresAt?: number
  consumed?: boolean
}

export interface SSHJournalEntry {
  id: number
  operationId: string
  sequence: number
  state: string
  event: string
  reasonCode?: string
  revision: string
  createdAt: number
}

const jsonOptions = { headers: { 'Content-Type': 'application/json' } }

export const getSSHPosture = () => HttpUtils.get('api/v1/operations/ssh/posture')
export const getSSHCapabilities = () => HttpUtils.get('api/v1/operations/ssh/capabilities')
export const getManagementEndpoints = () => HttpUtils.get('api/v1/operations/ssh/endpoints')
export const getRecoveryPaths = () => HttpUtils.get('api/v1/operations/ssh/recovery')
export const previewSSHPolicy = (policy: DesiredSSHPolicy, acknowledged: boolean) =>
  HttpUtils.post('api/v1/operations/ssh/preview', { policy, acknowledged }, jsonOptions)

export const startSSHCandidate = (payload: {
  policy: DesiredSSHPolicy
  idempotencyKey: string
  expectedPreviewRevision: string
  expectedPostureRevision: string
  expectedEndpointRevision: string
  expectedRecoveryRevision: string
  expectedProviderRevision: string
  endpointId: string
  principalId: string
  authenticationClass: 'publickey' | 'certificate'
  acknowledged: boolean
}, stepUpToken: string) => HttpUtils.post('api/v1/operations/ssh/candidate', payload, {
  headers: { 'Content-Type': 'application/json', 'X-Step-Up-Token': stepUpToken },
})

export const getSSHCandidate = (operationId: string) =>
  HttpUtils.get(`api/v1/operations/ssh/candidate/${encodeURIComponent(operationId)}/status`)

export const getSSHReconnectState = (operationId: string) =>
  HttpUtils.get(`api/v1/operations/ssh/candidate/${encodeURIComponent(operationId)}/reconnect`)

export const getSSHTimeline = (operationId: string) =>
  HttpUtils.get(`api/v1/operations/ssh/candidate/${encodeURIComponent(operationId)}/timeline`)

export const confirmSSHReconnect = (
  operationId: string,
  expectedRevision: number,
  providerEvidenceRef: string,
  stepUpToken: string,
) => HttpUtils.post(`api/v1/operations/ssh/candidate/${encodeURIComponent(operationId)}/reconnect/confirm`, {
  expectedRevision,
  providerEvidenceRef,
}, { headers: { 'Content-Type': 'application/json', 'X-Step-Up-Token': stepUpToken } })

export const rollbackSSHCandidate = (operationId: string, expectedRevision: number, stepUpToken: string) =>
  HttpUtils.post(`api/v1/operations/ssh/candidate/${encodeURIComponent(operationId)}/rollback`, {
    expectedRevision,
  }, { headers: { 'Content-Type': 'application/json', 'X-Step-Up-Token': stepUpToken } })

export const messageValue = <T>(message: Msg): T | null => {
  if (!message.success || !message.obj) return null
  return message.obj as T
}
