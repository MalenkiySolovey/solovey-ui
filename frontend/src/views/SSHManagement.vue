<template>
  <v-container class="ssh-management">
    <header class="d-flex flex-wrap align-center justify-space-between ga-3 mb-5">
      <div>
        <h1 class="text-h4">{{ t('sshManagement.title') }}</h1>
        <p class="text-body-1 mb-0">{{ t('sshManagement.subtitle') }}</p>
      </div>
      <v-btn variant="outlined" :loading="loading" @click="loadAll">
        {{ t('sshManagement.refresh') }}
      </v-btn>
    </header>

    <v-alert class="mb-3" type="warning" variant="tonal" role="status">
      <strong>{{ t('sshManagement.productionUnavailable') }}</strong>
      <div>{{ t('sshManagement.noHostMutation') }}</div>
    </v-alert>
    <v-alert v-if="errorMessage" class="mb-4" type="error" variant="tonal" role="alert">
      {{ errorMessage }}
    </v-alert>
    <v-progress-linear v-if="loading" indeterminate :aria-label="t('sshManagement.loading')" />

    <template v-else>
      <v-row>
        <v-col cols="12" lg="6">
          <v-card class="h-100">
            <v-card-title>{{ t('sshManagement.postureTitle') }}</v-card-title>
            <v-card-text>
              <v-alert v-if="!posture?.posture" type="warning" variant="tonal">
                {{ t('sshManagement.neverObserved') }}
              </v-alert>
              <template v-else>
                <v-chip :color="posture.fresh ? 'success' : 'warning'" class="mb-3">
                  {{ posture.fresh ? t('sshManagement.safe') : t('sshManagement.stale') }}
                </v-chip>
                <dl class="facts-grid">
                  <div><dt>Implementation</dt><dd>{{ safe(posture.posture.binary.implementation) }}</dd></div>
                  <div><dt>Version class</dt><dd>{{ safe(posture.posture.binary.versionClass) }}</dd></div>
                  <div><dt>Service</dt><dd>{{ safe(posture.posture.service.unitId) }}</dd></div>
                  <div><dt>{{ t('sshManagement.serviceManager') }}</dt><dd>{{ safe(posture.posture.service.manager) }}</dd></div>
                  <div><dt>{{ t('sshManagement.serviceState') }}</dt><dd>{{ safe(posture.posture.service.state) }}</dd></div>
                  <div><dt>{{ t('sshManagement.selectedBinary') }}</dt><dd>{{ yesNo(posture.posture.binary.selected) }}</dd></div>
                  <div><dt>{{ t('sshManagement.passwordAuthentication') }}</dt><dd>{{ safe(posture.posture.authentication.passwordAuthentication) }}</dd></div>
                  <div><dt>{{ t('sshManagement.kbdInteractiveAuthentication') }}</dt><dd>{{ safe(posture.posture.authentication.kbdInteractiveAuthentication) }}</dd></div>
                  <div><dt>{{ t('sshManagement.pubkeyAuthentication') }}</dt><dd>{{ safe(posture.posture.authentication.pubkeyAuthentication) }}</dd></div>
                  <div><dt>{{ t('sshManagement.permitRootLogin') }}</dt><dd>{{ safe(posture.posture.authentication.permitRootLogin) }}</dd></div>
                  <div><dt>{{ t('sshManagement.maxAuthTries') }}</dt><dd>{{ posture.posture.authentication.maxAuthTries }}</dd></div>
                  <div><dt>{{ t('sshManagement.loginGraceTime') }}</dt><dd>{{ posture.posture.authentication.loginGraceTimeSeconds }}</dd></div>
                  <div><dt>{{ t('sshManagement.authorizedKeyTemplates') }}</dt><dd>{{ posture.posture.authorizedKeys.pathTemplateCount }}</dd></div>
                  <div><dt>{{ t('sshManagement.hostKeyClasses') }}</dt><dd>{{ posture.posture.hostKeys.length }}</dd></div>
                  <div><dt>{{ t('sshManagement.expires') }}</dt><dd>{{ formatTime(posture.posture.expiresAt) }}</dd></div>
                </dl>
              </template>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" lg="6">
          <v-card class="h-100">
            <v-card-title>{{ t('sshManagement.capabilitiesTitle') }}</v-card-title>
            <v-card-text>
              <div v-for="item in capabilityRows" :key="item.name" class="d-flex justify-space-between ga-3 py-1">
                <span>{{ item.name }}</span>
                <v-chip size="small" :color="item.value === 'AVAILABLE' ? 'success' : item.value === 'UNAVAILABLE' ? 'warning' : 'default'">
                  {{ item.value }}
                </v-chip>
              </div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card v-if="posture?.posture" class="mt-5">
        <v-card-title>{{ t('sshManagement.configurationTitle') }}</v-card-title>
        <v-card-text class="table-scroll">
          <table class="status-table">
            <thead><tr><th>{{ t('sshManagement.sourceId') }}</th><th>{{ t('sshManagement.sourceKind') }}</th><th>{{ t('sshManagement.owner') }}</th><th>{{ t('sshManagement.modeClass') }}</th><th>{{ t('sshManagement.contextDepth') }}</th></tr></thead>
            <tbody>
              <tr v-for="node in posture.posture.configGraph" :key="node.id">
                <td>{{ safe(node.id) }}</td><td>{{ safe(node.kind) }}</td><td>{{ safe(node.owner) }}</td><td>{{ safe(node.modeClass) }}</td><td>{{ node.depth }}</td>
              </tr>
            </tbody>
          </table>
          <h2 class="text-subtitle-1 mt-5 mb-2">{{ t('sshManagement.matchContexts') }}</h2>
          <div v-for="context in posture.posture.matchContexts" :key="context.id" class="d-flex flex-wrap justify-space-between ga-3 py-1">
            <span>{{ safe(context.id) }} · {{ safe(context.conditionClass) }}</span><span>{{ context.known ? t('sshManagement.known') : t('sshManagement.unknown') }}</span>
          </div>
        </v-card-text>
      </v-card>

      <v-card class="mt-5">
        <v-card-title>{{ t('sshManagement.endpointsTitle') }}</v-card-title>
        <v-card-text class="table-scroll">
          <table class="status-table">
            <thead><tr><th>ID</th><th>{{ t('sshManagement.family') }}</th><th>{{ t('sshManagement.address') }}</th><th>{{ t('sshManagement.port') }}</th><th>{{ t('sshManagement.state') }}</th><th>{{ t('sshManagement.reasons') }}</th></tr></thead>
            <tbody>
              <tr v-for="endpoint in endpoints" :key="endpoint.id">
                <td>{{ endpoint.id }}</td><td>{{ safe(endpoint.family) }}</td><td>{{ safe(endpoint.bind) }}</td><td>{{ endpoint.port }}</td>
                <td>{{ endpoint.configuredIntent ? t('sshManagement.intent') : endpoint.observedListener ? t('sshManagement.listener') : 'UNKNOWN' }}</td>
                <td>{{ reasonText(endpoint.reasonCodes) }}</td>
              </tr>
              <tr v-if="endpoints.length === 0"><td colspan="6">{{ t('sshManagement.none') }}</td></tr>
            </tbody>
          </table>
        </v-card-text>
      </v-card>

      <v-card class="mt-5">
        <v-card-title>{{ t('sshManagement.recoveryTitle') }}</v-card-title>
        <v-card-text class="table-scroll">
          <table class="status-table">
            <thead><tr><th>ID</th><th>{{ t('sshManagement.method') }}</th><th>{{ t('sshManagement.independence') }}</th><th>{{ t('sshManagement.state') }}</th><th>{{ t('sshManagement.expires') }}</th></tr></thead>
            <tbody>
              <tr v-for="path in recoveryPaths" :key="path.id">
                <td>{{ path.id }}</td><td>{{ safe(path.verificationMethod) }}</td><td>{{ safe(path.independenceClass) }}</td><td>{{ safe(path.verificationState) }}</td><td>{{ formatTime(path.expiresAt) }}</td>
              </tr>
              <tr v-if="recoveryPaths.length === 0"><td colspan="5">{{ t('sshManagement.none') }}</td></tr>
            </tbody>
          </table>
        </v-card-text>
      </v-card>

      <v-card class="mt-5">
        <v-card-title>{{ t('sshManagement.policyTitle') }}</v-card-title>
        <v-card-subtitle>{{ t('sshManagement.policyDescription') }}</v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col cols="12" md="6"><v-text-field v-model.number="maxAuthTries" clearable type="number" min="1" max="20" :label="t('sshManagement.maxAuthTries')" /></v-col>
            <v-col cols="12" md="6"><v-text-field v-model.number="loginGraceTime" clearable type="number" min="1" max="600" :label="t('sshManagement.loginGraceTime')" /></v-col>
            <v-col v-for="field in booleanFields" :key="field.id" cols="12" md="4">
              <v-checkbox v-model="field.enabled.value" :label="field.label" hide-details />
              <v-select v-model="field.value.value" :disabled="!field.enabled.value" :items="booleanOptions" item-title="title" item-value="value" />
            </v-col>
            <v-col cols="12" md="6"><v-select v-model="rootLogin" :items="rootOptions" item-title="title" item-value="value" :label="t('sshManagement.permitRootLogin')" /></v-col>
          </v-row>
          <v-checkbox v-model="acknowledged" :label="t('sshManagement.acknowledge')" />
          <v-btn color="primary" :loading="working" @click="runPreview">{{ t('sshManagement.preview') }}</v-btn>
        </v-card-text>
      </v-card>

      <v-card v-if="preview" class="mt-5">
        <v-card-title><h2 ref="previewHeading" class="text-h6" tabindex="-1">{{ t('sshManagement.previewTitle') }}</h2></v-card-title>
        <v-card-text>
          <v-alert :type="preview.possible ? 'success' : 'warning'" variant="tonal" class="mb-4">
            {{ preview.possible ? t('sshManagement.safe') : t('sshManagement.unsafe') }}
          </v-alert>
          <dl class="facts-grid mb-4">
            <div><dt>{{ t('sshManagement.consoleVerified') }}</dt><dd>{{ yesNo(preview.preservation.consoleVerified) }}</dd></div>
            <div><dt>{{ t('sshManagement.pubkeyVerified') }}</dt><dd>{{ yesNo(preview.preservation.freshPubkeyReconnect) }}</dd></div>
            <div><dt>{{ t('sshManagement.watchdog') }}</dt><dd>{{ yesNo(preview.preservation.watchdogAvailable) }}</dd></div>
            <div><dt>{{ t('sshManagement.safetyExpiry') }}</dt><dd>{{ formatTime(preview.preservation.earliestSafetyExpiry) }}</dd></div>
            <div class="wide"><dt>{{ t('sshManagement.candidateDigest') }}</dt><dd class="digest">{{ preview.candidateDigest || 'UNAVAILABLE' }}</dd></div>
            <div class="wide"><dt>{{ t('sshManagement.reasons') }}</dt><dd>{{ reasonText(preview.reasonCodes) }}</dd></div>
          </dl>
          <h3 class="text-subtitle-1 mb-2">{{ t('sshManagement.semanticDiff') }}</h3>
          <div class="table-scroll mb-4">
            <table class="status-table">
              <thead><tr><th>{{ t('sshManagement.setting') }}</th><th>{{ t('sshManagement.before') }}</th><th>{{ t('sshManagement.after') }}</th></tr></thead>
              <tbody><tr v-for="row in policyDiffRows" :key="row.id"><td>{{ row.label }}</td><td>{{ row.before }}</td><td>{{ row.after }}</td></tr></tbody>
            </table>
          </div>
          <v-text-field v-model="securityCredential" :label="t('sshManagement.credential')" :hint="t('sshManagement.credentialHint')" persistent-hint type="password" autocomplete="current-password" />
          <v-btn color="warning" :disabled="!canApply" :loading="working" @click="applyCandidate">{{ t('sshManagement.apply') }}</v-btn>
          <p v-if="!canApply" class="text-body-2 mt-2">{{ t('sshManagement.applyUnavailable') }}</p>
        </v-card-text>
      </v-card>

      <v-card v-if="candidate" class="mt-5">
        <v-card-title class="d-flex flex-wrap align-center justify-space-between ga-2">
          <span>{{ t('sshManagement.candidateTitle') }}</span>
          <v-btn size="small" variant="outlined" :loading="candidateRefreshing" @click="refreshCandidate">
            {{ t('sshManagement.refreshCandidate') }}
          </v-btn>
        </v-card-title>
        <v-card-text>
          <dl class="facts-grid">
            <div><dt>ID</dt><dd>{{ candidate.operationId }}</dd></div>
            <div><dt>{{ t('sshManagement.state') }}</dt><dd>{{ candidate.state }}</dd></div>
            <div><dt>{{ t('sshManagement.revision') }}</dt><dd>{{ candidate.revision }}</dd></div>
            <div><dt>{{ t('sshManagement.rollbackAttempts') }}</dt><dd>{{ candidate.rollbackAttempts }}</dd></div>
            <div><dt>{{ t('sshManagement.watchdog') }}</dt><dd>{{ yesNo(candidate.preservation.watchdogAvailable) }}</dd></div>
            <div v-if="reconnectState"><dt>{{ t('sshManagement.reconnectRequired') }}</dt><dd>{{ yesNo(reconnectState.required) }}</dd></div>
            <div v-if="reconnectState"><dt>{{ t('sshManagement.proofConsumed') }}</dt><dd>{{ yesNo(Boolean(reconnectState.consumed)) }}</dd></div>
            <div v-if="reconnectState"><dt>{{ t('sshManagement.reconnectExpires') }}</dt><dd>{{ formatTime(reconnectState.expiresAt ?? 0) }}</dd></div>
          </dl>
          <v-alert v-if="candidate.restoredUntrusted" type="error" variant="tonal" class="mt-3">{{ t('sshManagement.restoredUntrusted') }}</v-alert>
          <v-alert v-if="manualRecoveryRequired" type="error" variant="tonal" class="mt-3">
            <strong>{{ t('sshManagement.manualRecoveryTitle') }}</strong>
            <div>{{ t('sshManagement.manualRecoveryDescription') }}</div>
            <div v-if="candidate.reasonCodes?.length" class="mt-1">{{ reasonText(candidate.reasonCodes) }}</div>
          </v-alert>
          <template v-if="candidate.state === 'RECONNECT_REQUIRED' && !reconnectState?.consumed">
            <v-alert type="warning" variant="tonal" class="mt-4">
              {{ t('sshManagement.confirmReconnectDescription') }}
            </v-alert>
            <v-text-field v-model.trim="providerEvidenceRef" class="mt-4"
              :label="t('sshManagement.providerEvidenceRef')" :hint="t('sshManagement.providerEvidenceHint')"
              persistent-hint type="password" autocomplete="off" spellcheck="false" />
            <v-text-field v-model="confirmCredential" :label="t('sshManagement.confirmCredential')"
              :hint="t('sshManagement.credentialHint')" persistent-hint type="password" autocomplete="current-password" />
            <v-btn color="warning" :disabled="!canConfirmReconnect" :loading="working" @click="confirmReconnectCandidate">
              {{ t('sshManagement.confirmReconnect') }}
            </v-btn>
          </template>
          <template v-if="canRollback">
            <v-text-field v-model="rollbackCredential" class="mt-4" :label="t('sshManagement.rollbackCredential')"
              :hint="t('sshManagement.credentialHint')" persistent-hint type="password" autocomplete="current-password" />
            <v-btn color="error" variant="outlined" :disabled="!rollbackCredential" :loading="working" @click="rollbackCandidate">
              {{ t('sshManagement.rollback') }}
            </v-btn>
          </template>
          <v-alert type="info" variant="tonal" class="mt-4">{{ safeNextAction }}</v-alert>
          <h3 class="text-subtitle-1 mt-5 mb-2">{{ t('sshManagement.operationTimeline') }}</h3>
          <div class="table-scroll">
            <table class="status-table">
              <thead><tr><th>#</th><th>{{ t('sshManagement.state') }}</th><th>{{ t('sshManagement.event') }}</th><th>{{ t('sshManagement.reasons') }}</th><th>{{ t('sshManagement.observedAt') }}</th></tr></thead>
              <tbody>
                <tr v-for="entry in timeline" :key="entry.sequence"><td>{{ entry.sequence }}</td><td>{{ safe(entry.state) }}</td><td>{{ safe(entry.event) }}</td><td>{{ safe(entry.reasonCode) }}</td><td>{{ formatTime(entry.createdAt) }}</td></tr>
                <tr v-if="timeline.length === 0"><td colspan="5">{{ t('sshManagement.none') }}</td></tr>
              </tbody>
            </table>
          </div>
        </v-card-text>
      </v-card>
    </template>

    <div class="sr-only" aria-live="polite">{{ liveStatus }}</div>
  </v-container>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'
import {
  getManagementEndpoints,
  getRecoveryPaths,
  getSSHCapabilities,
  getSSHCandidate,
  getSSHPosture,
  getSSHReconnectState,
  getSSHTimeline,
  confirmSSHReconnect,
  messageValue,
  previewSSHPolicy,
  rollbackSSHCandidate,
  startSSHCandidate,
  type DesiredSSHPolicy,
  type ManagementEndpoint,
  type RecoveryPath,
  type RootLoginPolicy,
  type SSHCandidate,
  type SSHCapabilities,
  type SSHPostureEnvelope,
  type SSHPreview,
  type SSHReconnectState,
  type SSHJournalEntry,
} from '@/shared/composables/useSSHManagement'

const { t } = useI18n()
const loading = ref(true)
const working = ref(false)
const candidateRefreshing = ref(false)
const errorMessage = ref('')
const liveStatus = ref('')
const posture = ref<SSHPostureEnvelope | null>(null)
const capabilities = ref<SSHCapabilities | null>(null)
const endpoints = ref<ManagementEndpoint[]>([])
const recoveryPaths = ref<RecoveryPath[]>([])
const preview = ref<SSHPreview | null>(null)
const candidate = ref<SSHCandidate | null>(null)
const reconnectState = ref<SSHReconnectState | null>(null)
const timeline = ref<SSHJournalEntry[]>([])
const previewHeading = ref<HTMLElement | null>(null)
const maxAuthTries = ref<number | null>(null)
const loginGraceTime = ref<number | null>(null)
const managePassword = ref(false)
const passwordValue = ref(true)
const manageKbd = ref(false)
const kbdValue = ref(true)
const managePubkey = ref(false)
const pubkeyValue = ref(true)
const rootLogin = ref<RootLoginPolicy>('UNCHANGED')
const acknowledged = ref(false)
const securityCredential = ref('')
const providerEvidenceRef = ref('')
const confirmCredential = ref('')
const rollbackCredential = ref('')
let candidateTimer: number | undefined

const booleanOptions = computed(() => [
  { title: t('sshManagement.enabled'), value: true },
  { title: t('sshManagement.disabled'), value: false },
])
const booleanFields = computed(() => [
  { id: 'password', label: t('sshManagement.passwordAuthentication'), enabled: managePassword, value: passwordValue },
  { id: 'kbd', label: t('sshManagement.kbdInteractiveAuthentication'), enabled: manageKbd, value: kbdValue },
  { id: 'pubkey', label: t('sshManagement.pubkeyAuthentication'), enabled: managePubkey, value: pubkeyValue },
])
const rootOptions = computed(() => [
  { title: t('sshManagement.unchanged'), value: 'UNCHANGED' },
  { title: 'yes', value: 'YES' },
  { title: 'no', value: 'NO' },
  { title: 'prohibit-password', value: 'PROHIBIT_PASSWORD' },
])
const capabilityRows = computed(() => {
  if (!capabilities.value) return []
  return Object.entries(capabilities.value)
    .filter(([key, value]) => key !== 'revision' && key !== 'reasonCodes' && typeof value === 'string')
    .map(([name, value]) => ({ name, value: value as string }))
})
const selectedSSHPath = computed(() => recoveryPaths.value.find(path => path.kind === 'SSH' && path.verificationState === 'verified'))
const selectedSSHEndpoint = computed(() => endpoints.value.find(endpoint => endpoint.serviceKind === 'SSH' && endpoint.id === selectedSSHPath.value?.endpointId))
const canApply = computed(() => Boolean(
  preview.value?.possible && capabilities.value?.stage === 'AVAILABLE' && capabilities.value.reload === 'AVAILABLE' &&
  selectedSSHPath.value && selectedSSHEndpoint.value && securityCredential.value,
))
const terminalCandidateStates = new Set(['COMMITTED', 'ROLLED_BACK', 'MANUAL_RECOVERY_REQUIRED'])
const canRollback = computed(() => Boolean(candidate.value && !terminalCandidateStates.has(candidate.value.state) &&
  candidate.value.state !== 'DRAFT' && candidate.value.state !== 'PREFLIGHTED'))
const canConfirmReconnect = computed(() => Boolean(
  candidate.value?.state === 'RECONNECT_REQUIRED' && reconnectState.value?.required && !reconnectState.value?.consumed &&
  providerEvidenceRef.value.trim() && confirmCredential.value,
))
const manualRecoveryRequired = computed(() => candidate.value?.state === 'MANUAL_RECOVERY_REQUIRED')
const safeNextAction = computed(() => {
  switch (candidate.value?.state) {
    case 'RECONNECT_REQUIRED': return t('sshManagement.nextReconnect')
    case 'ROLLED_BACK': return t('sshManagement.nextRolledBack')
    case 'MANUAL_RECOVERY_REQUIRED': return t('sshManagement.nextManualRecovery')
    case 'COMMITTED': return t('sshManagement.nextCommitted')
    default: return t('sshManagement.nextWait')
  }
})
const policyDiffRows = computed(() => {
  const desired = preview.value?.policy
  const current = preview.value?.posture?.authentication
  if (!desired) return []
  const unchanged = t('sshManagement.unchanged')
  return [
    { id: 'maxAuthTries', label: t('sshManagement.maxAuthTries'), before: safe(current?.maxAuthTries), after: desired.maxAuthTries ?? unchanged },
    { id: 'loginGraceTimeSeconds', label: t('sshManagement.loginGraceTime'), before: safe(current?.loginGraceTimeSeconds), after: desired.loginGraceTimeSeconds ?? unchanged },
    { id: 'passwordAuthentication', label: t('sshManagement.passwordAuthentication'), before: safe(current?.passwordAuthentication), after: policyValue(desired.passwordAuthentication, unchanged) },
    { id: 'kbdInteractiveAuthentication', label: t('sshManagement.kbdInteractiveAuthentication'), before: safe(current?.kbdInteractiveAuthentication), after: policyValue(desired.kbdInteractiveAuthentication, unchanged) },
    { id: 'permitRootLogin', label: t('sshManagement.permitRootLogin'), before: safe(current?.permitRootLogin), after: desired.permitRootLogin === 'UNCHANGED' ? unchanged : desired.permitRootLogin },
    { id: 'pubkeyAuthentication', label: t('sshManagement.pubkeyAuthentication'), before: safe(current?.pubkeyAuthentication), after: policyValue(desired.pubkeyAuthentication, unchanged) },
  ]
})

const policy = (): DesiredSSHPolicy => {
  const value: DesiredSSHPolicy = { schema: 'solovey-ui/ssh-managed-policy/v1', permitRootLogin: rootLogin.value }
  if (maxAuthTries.value !== null && maxAuthTries.value !== undefined) value.maxAuthTries = Number(maxAuthTries.value)
  if (loginGraceTime.value !== null && loginGraceTime.value !== undefined) value.loginGraceTimeSeconds = Number(loginGraceTime.value)
  if (managePassword.value) value.passwordAuthentication = passwordValue.value
  if (manageKbd.value) value.kbdInteractiveAuthentication = kbdValue.value
  if (managePubkey.value) value.pubkeyAuthentication = pubkeyValue.value
  return value
}

const loadAll = async () => {
  loading.value = true
  errorMessage.value = ''
  const [postureResponse, capabilityResponse, endpointResponse, recoveryResponse] = await Promise.all([
    getSSHPosture(), getSSHCapabilities(), getManagementEndpoints(), getRecoveryPaths(),
  ])
  posture.value = messageValue<SSHPostureEnvelope>(postureResponse)
  capabilities.value = messageValue<SSHCapabilities>(capabilityResponse)
  endpoints.value = messageValue<{ items: ManagementEndpoint[] }>(endpointResponse)?.items ?? []
  recoveryPaths.value = messageValue<{ items: RecoveryPath[] }>(recoveryResponse)?.items ?? []
  const failed = [postureResponse, capabilityResponse, endpointResponse, recoveryResponse].find(response => !response.success)
  errorMessage.value = failed?.msg ?? ''
  loading.value = false
  liveStatus.value = errorMessage.value || t('sshManagement.refresh')
}

const runPreview = async () => {
  working.value = true
  errorMessage.value = ''
  const response = await previewSSHPolicy(policy(), acknowledged.value)
  preview.value = messageValue<SSHPreview>(response)
  errorMessage.value = response.success ? '' : response.msg
  working.value = false
  liveStatus.value = errorMessage.value || (preview.value?.possible ? t('sshManagement.safe') : t('sshManagement.unsafe'))
  await nextTick()
  previewHeading.value?.focus()
}

const applyCandidate = async () => {
  const current = preview.value
  const endpoint = selectedSSHEndpoint.value
  const path = selectedSSHPath.value
  if (!current || !endpoint || !path || !current.postureRevision) return
  working.value = true
  errorMessage.value = ''
  const target = `ssh-candidate:${current.revision}`
  const stepUp = await acquireStepUpToken('ssh.candidate.apply', target, securityCredential.value)
  if (!stepUp.token) {
    errorMessage.value = stepUp.response.msg
    working.value = false
    return
  }
  const response = await startSSHCandidate({
    policy: current.policy,
    idempotencyKey: `ssh-ui:${crypto.randomUUID()}`,
    expectedPreviewRevision: current.revision,
    expectedPostureRevision: current.postureRevision,
    expectedEndpointRevision: current.endpointRevision,
    expectedRecoveryRevision: current.recoveryRevision,
    expectedProviderRevision: current.providerRevision ?? '',
    endpointId: endpoint.id,
    principalId: path.principalId,
    authenticationClass: 'publickey',
    acknowledged: acknowledged.value,
  }, stepUp.token)
  candidate.value = messageValue<SSHCandidate>(response)
  errorMessage.value = response.success ? '' : response.msg
  securityCredential.value = ''
  working.value = false
  liveStatus.value = errorMessage.value || candidate.value?.state || ''
  if (candidate.value) await refreshCandidate()
}

const refreshCandidate = async () => {
  const operationId = candidate.value?.operationId
  if (!operationId) return
  candidateRefreshing.value = true
  const [candidateResponse, reconnectResponse, timelineResponse] = await Promise.all([
    getSSHCandidate(operationId), getSSHReconnectState(operationId), getSSHTimeline(operationId),
  ])
  const refreshed = messageValue<SSHCandidate>(candidateResponse)
  if (refreshed) candidate.value = refreshed
  reconnectState.value = messageValue<SSHReconnectState>(reconnectResponse)
  timeline.value = messageValue<{ items: SSHJournalEntry[] }>(timelineResponse)?.items ?? []
  const failed = [candidateResponse, reconnectResponse, timelineResponse].find(response => !response.success)
  errorMessage.value = failed?.msg ?? ''
  candidateRefreshing.value = false
  liveStatus.value = errorMessage.value || candidate.value?.state || ''
}

const rollbackCandidate = async () => {
  const current = candidate.value
  if (!current || !canRollback.value || !rollbackCredential.value) return
  working.value = true
  errorMessage.value = ''
  const target = `ssh-operation:${current.operationId}:${current.revision}`
  const stepUp = await acquireStepUpToken('ssh.candidate.rollback', target, rollbackCredential.value)
  if (!stepUp.token) {
    errorMessage.value = stepUp.response.msg
    rollbackCredential.value = ''
    working.value = false
    return
  }
  const response = await rollbackSSHCandidate(current.operationId, current.revision, stepUp.token)
  const rolledBack = messageValue<SSHCandidate>(response)
  if (rolledBack) candidate.value = rolledBack
  errorMessage.value = response.success ? '' : response.msg
  rollbackCredential.value = ''
  working.value = false
  liveStatus.value = errorMessage.value || candidate.value?.state || ''
  await refreshCandidate()
}

const confirmReconnectCandidate = async () => {
  const current = candidate.value
  const evidenceRef = providerEvidenceRef.value.trim()
  if (!current || !canConfirmReconnect.value || !evidenceRef || !confirmCredential.value) return
  working.value = true
  errorMessage.value = ''
  const target = `ssh-operation:${current.operationId}:${current.revision}`
  const stepUp = await acquireStepUpToken('ssh.candidate.confirm', target, confirmCredential.value)
  if (!stepUp.token) {
    errorMessage.value = stepUp.response.msg
    confirmCredential.value = ''
    working.value = false
    return
  }
  const response = await confirmSSHReconnect(current.operationId, current.revision, evidenceRef, stepUp.token)
  const confirmed = messageValue<SSHCandidate>(response)
  if (confirmed) candidate.value = confirmed
  errorMessage.value = response.success ? '' : response.msg
  providerEvidenceRef.value = ''
  confirmCredential.value = ''
  working.value = false
  liveStatus.value = errorMessage.value || candidate.value?.state || ''
  await refreshCandidate()
}

const safe = (value: unknown) => typeof value === 'number' ? String(value) : typeof value === 'string' && value.trim() ? value : 'UNKNOWN'
const policyValue = (value: boolean | undefined, unchanged: string) => value === undefined ? unchanged : value ? 'yes' : 'no'
const reasonText = (values?: string[]) => values?.length ? values.join(', ') : t('sshManagement.none')
const yesNo = (value: boolean) => value ? t('yes') : t('no')
const formatTime = (unix: number) => unix > 0 ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(unix * 1000)) : 'UNKNOWN'

onMounted(() => {
  void loadAll()
  candidateTimer = window.setInterval(() => {
    if (candidate.value && !terminalCandidateStates.has(candidate.value.state)) void refreshCandidate()
  }, 5000)
})
onUnmounted(() => {
  if (candidateTimer !== undefined) window.clearInterval(candidateTimer)
})
</script>

<style scoped>
.facts-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
.facts-grid dt { font-size: .75rem; opacity: .75; }
.facts-grid dd { margin: 0; overflow-wrap: anywhere; }
.facts-grid .wide { grid-column: 1 / -1; }
.digest { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
.table-scroll { overflow-x: auto; }
.status-table { width: 100%; border-collapse: collapse; }
.status-table th, .status-table td { padding: .65rem; text-align: left; vertical-align: top; border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); overflow-wrap: anywhere; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@media (max-width: 600px) { .facts-grid { grid-template-columns: 1fr; } .facts-grid .wide { grid-column: auto; } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; } }
</style>
