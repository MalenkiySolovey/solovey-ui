import HttpUtils, { type Msg } from '@/plugins/httputil'

export type DeploymentProfileID =
  | 'native-hardened'
  | 'native-network-advanced'
  | 'native-legacy-root'
  | 'docker-host-unprivileged'
  | 'docker-bridge-explicit'
  | 'docker-network-advanced'

export interface DeploymentProfile {
  id: DeploymentProfileID
  runtime: 'native' | 'docker'
  support: string
  freshInstallDefault: boolean
  panelRoot: boolean
  brokerRequired: boolean
  hostNetwork: boolean
  explicitPorts: boolean
  networkCapabilities?: string[]
  processIdentities: string[]
  writeScopes: string[]
  serviceUnits: string[]
  evidenceStatus: string
  constraints?: string[]
  revision: string
}

export interface DeploymentCapabilities {
  observe: string
  doctor: string
  migrate: string
  rollback: string
  reasons?: string[]
  revision: string
}

export interface SystemdActualState {
  schema: string
  version: string
  managerBootRevision: string
  directiveSupport: string
  directiveCapabilityRevision: string
  unit: string
  fragmentRevision: string
  dropInRevision: string
  unitFileState: string
  loadState: string
  activeState: string
  subState: string
  daemonReloadRequired: boolean
  user: string
  group: string
  boundingCapabilities: string[]
  ambientCapabilities: string[]
  noNewPrivileges: boolean
  sandboxRevision: string
  writePaths: string[]
  readOnlyPaths: string[]
  executableRevision: string
  runtimeDirectoryRevision: string
  resourceRevision: string
  restart: string
  restartUSec: string
  watchdogUSec: string
  brokerSocketRevision: string
  observedAt: number
  expiresAt: number
  revision: string
}

export interface DeploymentPosture {
  profile: DeploymentProfileID
  installedProfile: DeploymentProfileID
  activeProfile: DeploymentProfileID
  verifiedProfile?: DeploymentProfileID
  runtime: 'native' | 'docker'
  panelUid: number
  panelGid: number
  panelRoot: boolean
  brokerAvailable: boolean
  brokerRevision?: string
  serviceRevision: string
  dataRevision: string
  hardeningRevision: string
  systemd?: SystemdActualState
  observedAt: number
  expiresAt: number
  revision: string
  reasons?: string[]
}

export interface DeploymentStatus {
  state: string
  posture: DeploymentPosture
  desiredProfile: DeploymentProfileID
  generatedProfile: DeploymentProfileID
  generatedRevision: string
  installedProfile: DeploymentProfileID
  activeProfile: DeploymentProfileID
  verifiedProfile: DeploymentProfileID
  compatibilityState: string
  doctorRevision: string
  trusted: boolean
  evidenceStatus: string
}

export interface DeploymentFinding {
  code: string
  severity: 'INFO' | 'WARNING' | 'CRITICAL'
  messageKey: string
  remediation: string
}

export interface DeploymentDoctor {
  posture?: DeploymentPosture
  capabilities: DeploymentCapabilities
  profiles: DeploymentProfile[]
  findings: DeploymentFinding[]
  healthy: boolean
  state: string
  desiredProfile?: DeploymentProfileID
  generatedProfile?: DeploymentProfileID
  installedProfile?: DeploymentProfileID
  activeProfile?: DeploymentProfileID
  verifiedProfile?: DeploymentProfileID
  evidenceStatus: string
  generatedAt: number
  revision: string
}

export interface DeploymentBroker {
  available: boolean
  protocolRevision: string
  transport: string
  peerPosture: string
  capabilities: DeploymentCapabilities
}

export interface DeploymentPreview {
  posture: DeploymentPosture
  target: DeploymentProfile
  doctor: DeploymentDoctor
  possible: boolean
  reasons?: string[]
  revision: string
}

export interface DeploymentOperation {
  operationId: string
  state: string
  fromProfile: DeploymentProfileID
  targetProfile: DeploymentProfileID
  revision: number
  restoredUntrusted: boolean
  reconciledAt?: number
  createdAt: number
  updatedAt: number
  reasonCodes?: string[]
  rollbackAvailable: boolean
}

export interface DeploymentRecovery {
  required: boolean
  operation?: DeploymentOperation
  reasonCodes?: string[]
}

export interface DeploymentTimeline {
  sequence: number
  state: string
  event: string
  reasonCode?: string
  revision: string
  createdAt: number
}

const jsonHeaders = { 'Content-Type': 'application/json' }

export const getDeploymentProfiles = () => HttpUtils.get('api/v1/operations/deployment/profiles')
export const getDeploymentStatus = () => HttpUtils.get('api/v1/operations/deployment/status')
export const getDeploymentDoctor = () => HttpUtils.get('api/v1/operations/deployment/doctor')
export const getDeploymentBroker = () => HttpUtils.get('api/v1/operations/deployment/broker')
export const getDeploymentCapabilities = () => HttpUtils.get('api/v1/operations/deployment/capabilities')
export const getDeploymentRecovery = () => HttpUtils.get('api/v1/operations/deployment/recovery')
export const previewDeployment = (targetProfile: DeploymentProfileID, acknowledged: boolean) =>
  HttpUtils.post('api/v1/operations/deployment/preview', { targetProfile, acknowledged }, { headers: jsonHeaders })
export const startDeploymentMigration = (payload: Record<string, unknown>, stepUpToken: string) =>
  HttpUtils.post('api/v1/operations/deployment/migration', payload, { headers: { ...jsonHeaders, 'X-Step-Up-Token': stepUpToken } })
export const getDeploymentOperation = (operationId: string) =>
  HttpUtils.get(`api/v1/operations/deployment/migration/${encodeURIComponent(operationId)}/status`)
export const getDeploymentTimeline = (operationId: string) =>
  HttpUtils.get(`api/v1/operations/deployment/migration/${encodeURIComponent(operationId)}/timeline`)
export const confirmDeploymentMigration = (operationId: string, expectedRevision: number, stepUpToken: string) =>
  HttpUtils.post(`api/v1/operations/deployment/migration/${encodeURIComponent(operationId)}/confirm`, { expectedRevision },
    { headers: { ...jsonHeaders, 'X-Step-Up-Token': stepUpToken } })
export const rollbackDeploymentMigration = (operationId: string, expectedRevision: number, stepUpToken: string) =>
  HttpUtils.post(`api/v1/operations/deployment/migration/${encodeURIComponent(operationId)}/rollback`, { expectedRevision },
    { headers: { ...jsonHeaders, 'X-Step-Up-Token': stepUpToken } })

export const deploymentMessage = <T>(message: Msg): T | null =>
  message.success && message.obj ? message.obj as T : null
