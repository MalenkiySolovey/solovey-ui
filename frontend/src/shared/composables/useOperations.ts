import HttpUtils, { type Msg } from '@/plugins/httputil'

export interface UpdateCapabilities {
  mode: string
  check: string
  download: string
  prepare: string
  activate: string
  rollback: string
  osUpdates: string
  reboot: string
  reasonCodes: string[]
  revision: string
}

export interface UpdateOperation {
  operationId: string
  state: string
  channel: string
  sequence: number
  releaseId: string
  version: string
  manifestDigest: string
  artifactSetDigest: string
  binaryProfile: string
  deploymentRevision: string
  brokerCapability: string
  migrationSetDigest: string
  backupRef: string
  restartClass: string
  rebootClass: string
  rollbackClass: string
  bytesCompleted: number
  bytesTotal: number
  reasonCode: string
  revision: number
  restoredUntrusted: boolean
  rollbackAvailable: boolean
  createdAt: number
  updatedAt: number
}

export interface UpdatePosture {
  schema: string
  state: string
  signingStatus: string
  desired: { channel: 'main' | 'beta' }
  selected?: {
    channel: string
    releaseId: string
    version: string
    sequence: number
    manifestDigest: string
    signingKeyId: string
    verifiedAt: number
  }
  actual: { version: string; binaryProfile: string; mode: string }
  operation?: UpdateOperation
  capabilities: UpdateCapabilities
  reasonCodes: string[]
  observedAt: number
  freshUntil?: number
}

export interface UpdateCheck {
  state: string
  currentVersion: string
  releaseId?: string
  version?: string
  channel: 'main' | 'beta'
  sequence?: number
  manifestDigest?: string
  signingKeyId?: string
  signingStatus: string
  updateAvailable: boolean
  artifactSetDigest?: string
  restartClass?: string
  rebootClass?: string
  rollbackClass?: string
  reasonCodes: string[]
  capabilities: UpdateCapabilities
}

export interface UpdateTimelineEntry {
  sequence: number
  operationId: string
  state: string
  event: string
  reasonCode: string
  revision: number
  semanticHash: string
  createdAt: number
}

export interface PressureSignal {
  id: string
  status: string
  value?: number
  unit?: string
  observedAt?: number
  expiresAt?: number
  reasonCode?: string
}

export interface PressureThreshold {
  id: string
  direction: string
  warning: number
  constrained: number
  critical: number
  required: boolean
}

export interface PressureAdmission {
  allowed: boolean
  reasonCode: string
  retryAfterSeconds?: number
}

export interface ResourcePressurePosture {
  schema: string
  desired: { thresholds: PressureThreshold[]; sampleIntervalSeconds: number; recoveryWindowSeconds: number }
  selected: { signals: PressureSignal[] }
  actual: {
    state: string
    previousState: string
    signals: PressureSignal[]
    reasonCodes: string[]
    observationDigest: string
    revision: number
    observedAt: number
    changedAt: number
  }
  state: string
  previousState: string
  reasonCodes: string[]
  revision: number
  observedAt: number
  freshUntil: number
  admissionEffects: Record<string, PressureAdmission>
  limitations: string[]
}

export interface SQLitePosture {
  provider: string
  moduleVersion: string
  moduleCommit: string
  runtimeVersion: string
  sourceId: string
  compileOptions: string[]
  journalMode: string
  walCapable: boolean
  walResetSafe: boolean
  safetyClass: string
  revision: string
}

export interface MigrationJournal {
  scope: string
  ownerId: string
  stepId: string
  checksum: string
  state: string
  compatibilityState: string
  errorCode: string
  updatedAt: number
}

export interface MigrationPosture {
  state: string
  items: MigrationJournal[]
  limit: number
  truncated: boolean
}

export interface DataOwner {
  id: string
  kind: string
  installed: boolean
  available: boolean
  enabled: string
  durableData: string
  backup: string
  restore: string
  dropData: string
}

export interface DataOwnersPosture {
  state: string
  items: DataOwner[]
  reasonCodes: string[]
}

export interface DropResource {
  id: string
  kind: string
  rows?: number
  owner: string
  terminalState?: string
  class?: string
}

export interface DropPreview {
  schema: string
  ownerId: string
  installed: boolean
  available: boolean
  enabled: boolean
  resources: DropResource[]
  operations: string[]
  leaseCount: number
  externalAuthority: string
  backupRequired: boolean
  blockers: string[]
  postcondition: string
  revision: string
  generatedAt: number
}

export interface BackupOwner {
  id: string
  installed: boolean
  available: boolean
  mode: string
}

export interface RestoreOwner {
  id: string
  installed: boolean
  available: boolean
  included: boolean
  mode: string
  schemaVersion?: string
  manifestChecksum?: string
  compatibility: string
  hookStatus: string
}

export interface RestoreRehearsal {
  schema: string
  state: string
  possible: boolean
  backupDigest: string
  backupBytes: number
  manifestStatus: string
  manifest?: {
    backupId: string
    appVersion: string
    coreSchema: string
    sqliteRuntime: string
    sqliteSourceId: string
    releaseSequence: number
    releaseVersion: string
    owners: BackupOwner[]
    tables: Array<{ owner: string; name: string; rows: number; excluded: boolean; exclusionCode?: string }>
  }
  integrity: string
  schemaCompatibility: string
  migrationPlan: string
  releaseCompatibility: string
  spaceStatus: string
  owners: RestoreOwner[]
  reasonCodes: string[]
  revision: string
  generatedAt: number
}

export interface OperationsStatus {
  schema: string
  generatedAt: number
  security: { state: string; accepted: boolean; live: boolean }
  deployment: { state: string; posture: unknown }
  update: UpdatePosture
  pressure: ResourcePressurePosture['actual']
  sqlite: { state: string; runtime: SQLitePosture }
  migrations: { state: string; journalRows: number; targetCoreSchema: string }
  owners: DataOwner[]
  backup: { state: string; restoreExecution: string }
  restore: { rehearsal: string; execution: string }
  dropData: { state: string; force: boolean }
  evidence: { normalCI: string; live: string; accepted: boolean }
  reasonCodes: string[]
}

const jsonHeaders = { 'Content-Type': 'application/json' }
const stepUpHeaders = (token: string) => ({ ...jsonHeaders, 'X-Step-Up-Token': token })

export const operationsMessage = <T>(message: Msg): T | null =>
  message.success && message.obj ? message.obj as T : null

export const getOperationsStatus = () => HttpUtils.get('api/v1/operations/status')
export const getUpdatePosture = (channel: 'main' | 'beta' = 'main') =>
  HttpUtils.get('api/v1/operations/update/posture', { channel })
export const getUpdateRecovery = () => HttpUtils.get('api/v1/operations/update/recovery')
export const checkSignedUpdate = (channel: 'main' | 'beta') =>
  HttpUtils.post('api/v1/operations/update/check', { channel }, { headers: jsonHeaders })
export const prepareSignedUpdate = (payload: Record<string, unknown>, token: string) =>
  HttpUtils.post('api/v1/operations/update/preflight', payload, { headers: stepUpHeaders(token) })
export const activateSignedUpdate = (payload: Record<string, unknown>, token: string) =>
  HttpUtils.post('api/v1/operations/update/activate', payload, { headers: stepUpHeaders(token) })
export const rollbackSignedUpdate = (payload: Record<string, unknown>, token: string) =>
  HttpUtils.post('api/v1/operations/update/rollback', payload, { headers: stepUpHeaders(token) })
export const getUpdateTimeline = (operationId: string, after = 0, limit = 100) =>
  HttpUtils.get(`api/v1/operations/update/operations/${encodeURIComponent(operationId)}/timeline`, { after, limit })

export const getResourcePressure = () => HttpUtils.get('api/v1/operations/resource-pressure')
export const getSQLitePosture = () => HttpUtils.get('api/v1/operations/sqlite')
export const getMigrationPosture = () => HttpUtils.get('api/v1/operations/migrations')
export const getDataOwners = () => HttpUtils.get('api/v1/operations/data/owners')
export const previewDropData = (ownerId: string) =>
  HttpUtils.post('api/v1/operations/data/drop/preview', { ownerId }, { headers: jsonHeaders })
export const executeDropData = (payload: Record<string, unknown>, token: string) =>
  HttpUtils.post('api/v1/operations/data/drop', payload, { headers: stepUpHeaders(token) })
export const rehearseRestore = (form: FormData) =>
  HttpUtils.post('api/v1/operations/data/restore/rehearsal', form)
export const executeRestore = (form: FormData, token: string) =>
  HttpUtils.post('api/v1/operations/data/restore', form, { headers: { 'X-Step-Up-Token': token } })

export const updateConfirmation = (action: 'PREPARE' | 'ACTIVATE' | 'ROLLBACK', sequence: number) =>
  `${action}_UPDATE_${sequence}`
export const dropConfirmation = (ownerId: string) =>
  `DROP_DATA_${ownerId.toUpperCase().replaceAll('-', '_')}`
export const restoreConfirmation = (revision: string) =>
  revision.length >= 12 ? `RESTORE_DATABASE_${revision.slice(0, 12).toUpperCase()}` : ''

export const safeOperationalValue = (value: unknown): string => {
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  if (typeof value !== 'string') return 'UNKNOWN'
  const normalized = value.trim()
  return normalized && normalized.length <= 256 ? normalized : 'UNKNOWN'
}

export const safeReasonCodes = (values?: string[]): string[] =>
  (values ?? []).filter(value => /^[a-zA-Z0-9_.:+-]{1,160}$/.test(value)).slice(0, 32)

export const isFresh = (freshUntil: number | undefined, nowSeconds = Date.now() / 1000): boolean =>
  typeof freshUntil === 'number' && freshUntil > nowSeconds
