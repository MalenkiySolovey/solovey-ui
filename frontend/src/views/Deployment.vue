<template>
  <v-container class="deployment-page">
    <header class="d-flex flex-wrap align-center justify-space-between ga-3 mb-5">
      <div><h1 class="text-h4">{{ t('deployment.title') }}</h1><p class="mb-0">{{ t('deployment.subtitle') }}</p></div>
      <v-btn variant="outlined" :loading="loading" @click="loadAll">{{ t('deployment.refresh') }}</v-btn>
    </header>
    <v-alert v-if="errorMessage" type="warning" variant="tonal" class="mb-4" role="alert">{{ errorMessage }}</v-alert>
    <v-progress-linear v-if="loading" indeterminate :aria-label="t('deployment.loading')" />

    <template v-else>
      <v-row>
        <v-col cols="12" lg="6"><v-card class="h-100"><v-card-title>{{ t('deployment.posture') }}</v-card-title><v-card-text>
          <v-alert v-if="!status" type="warning" variant="tonal">{{ t('deployment.notObserved') }}</v-alert>
          <dl v-else class="facts-grid">
            <div><dt>{{ t('deployment.kind') }}</dt><dd>{{ status.posture.runtime }}</dd></div>
			<div><dt>{{ t('deployment.state') }}</dt><dd>{{ safe(status.state) }}</dd></div>
            <div><dt>{{ t('deployment.desired') }}</dt><dd>{{ status.desiredProfile }}</dd></div>
            <div><dt>{{ t('deployment.generated') }}</dt><dd>{{ status.generatedProfile }}</dd></div>
            <div><dt>{{ t('deployment.installed') }}</dt><dd>{{ status.installedProfile }}</dd></div>
            <div><dt>{{ t('deployment.active') }}</dt><dd>{{ status.activeProfile }}</dd></div>
            <div><dt>{{ t('deployment.verified') }}</dt><dd>{{ status.verifiedProfile }}</dd></div>
            <div><dt>{{ t('deployment.compatibility') }}</dt><dd>{{ status.compatibilityState }}</dd></div>
			<div><dt>{{ t('deployment.evidence') }}</dt><dd>{{ safe(status.evidenceStatus) }}</dd></div>
            <div><dt>{{ t('deployment.processIdentity') }}</dt><dd>UID {{ status.posture.panelUid }} / GID {{ status.posture.panelGid }}</dd></div>
            <div><dt>{{ t('deployment.rootProcess') }}</dt><dd>{{ yesNo(status.posture.panelRoot) }}</dd></div>
            <div><dt>{{ t('deployment.hardening') }}</dt><dd class="digest">{{ status.posture.hardeningRevision }}</dd></div>
            <div><dt>{{ t('deployment.observed') }}</dt><dd>{{ formatTime(status.posture.observedAt) }}</dd></div>
          </dl>
        </v-card-text></v-card></v-col>
        <v-col cols="12" lg="6"><v-card class="h-100"><v-card-title>{{ t('deployment.broker') }}</v-card-title><v-card-text>
          <v-chip :color="broker?.available ? 'success' : 'warning'" class="mb-3">{{ broker?.available ? t('deployment.available') : t('deployment.unavailable') }}</v-chip>
          <dl class="facts-grid">
            <div><dt>{{ t('deployment.protocol') }}</dt><dd>{{ safe(broker?.protocolRevision) }}</dd></div>
            <div><dt>{{ t('deployment.transport') }}</dt><dd>{{ safe(broker?.transport) }}</dd></div>
            <div><dt>{{ t('deployment.peerPosture') }}</dt><dd>{{ safe(broker?.peerPosture) }}</dd></div>
            <div><dt>{{ t('deployment.migrationCapability') }}</dt><dd>{{ safe(capabilities?.migrate) }}</dd></div>
            <div><dt>{{ t('deployment.rollbackCapability') }}</dt><dd>{{ safe(capabilities?.rollback) }}</dd></div>
            <div><dt>{{ t('deployment.reasons') }}</dt><dd>{{ reasons(capabilities?.reasons) }}</dd></div>
          </dl>
        </v-card-text></v-card></v-col>
      </v-row>

      <v-card v-if="status?.posture.runtime === 'native'" class="mt-5"><v-card-title>{{ t('deployment.systemdActual') }}</v-card-title><v-card-text>
        <v-alert v-if="!status.posture.systemd" type="warning" variant="tonal">{{ t('deployment.systemdUnavailable') }}</v-alert>
        <template v-else>
          <dl class="facts-grid">
            <div><dt>{{ t('deployment.systemdVersion') }}</dt><dd>{{ status.posture.systemd.version }}</dd></div>
            <div><dt>{{ t('deployment.directiveSupport') }}</dt><dd>{{ status.posture.systemd.directiveSupport }}</dd></div>
            <div><dt>{{ t('deployment.unit') }}</dt><dd>{{ status.posture.systemd.unit }}</dd></div>
            <div><dt>{{ t('deployment.unitState') }}</dt><dd>{{ status.posture.systemd.unitFileState }} / {{ status.posture.systemd.loadState }} / {{ status.posture.systemd.activeState }} / {{ status.posture.systemd.subState }}</dd></div>
            <div><dt>{{ t('deployment.daemonReloadRequired') }}</dt><dd>{{ yesNo(status.posture.systemd.daemonReloadRequired) }}</dd></div>
            <div><dt>{{ t('deployment.serviceIdentity') }}</dt><dd>{{ status.posture.systemd.user }} : {{ status.posture.systemd.group }}</dd></div>
            <div><dt>{{ t('deployment.noNewPrivileges') }}</dt><dd>{{ yesNo(status.posture.systemd.noNewPrivileges) }}</dd></div>
            <div><dt>{{ t('deployment.boundingCapabilities') }}</dt><dd>{{ reasons(status.posture.systemd.boundingCapabilities) }}</dd></div>
            <div><dt>{{ t('deployment.ambientCapabilities') }}</dt><dd>{{ reasons(status.posture.systemd.ambientCapabilities) }}</dd></div>
            <div><dt>{{ t('deployment.writePaths') }}</dt><dd>{{ reasons(status.posture.systemd.writePaths) }}</dd></div>
            <div><dt>{{ t('deployment.readOnlyPaths') }}</dt><dd>{{ reasons(status.posture.systemd.readOnlyPaths) }}</dd></div>
            <div><dt>{{ t('deployment.restartPolicy') }}</dt><dd>{{ status.posture.systemd.restart }} / {{ status.posture.systemd.restartUSec }}</dd></div>
            <div><dt>{{ t('deployment.watchdog') }}</dt><dd>{{ status.posture.systemd.watchdogUSec }}</dd></div>
            <div><dt>{{ t('deployment.systemdObserved') }}</dt><dd>{{ formatTime(status.posture.systemd.observedAt) }}</dd></div>
            <div><dt>{{ t('deployment.systemdExpires') }}</dt><dd>{{ formatTime(status.posture.systemd.expiresAt) }}</dd></div>
            <div><dt>{{ t('deployment.directiveRevision') }}</dt><dd class="digest">{{ status.posture.systemd.directiveCapabilityRevision }}</dd></div>
            <div><dt>{{ t('deployment.unitRevision') }}</dt><dd class="digest">{{ status.posture.systemd.fragmentRevision }}</dd></div>
            <div><dt>{{ t('deployment.dropInRevision') }}</dt><dd class="digest">{{ status.posture.systemd.dropInRevision }}</dd></div>
            <div><dt>{{ t('deployment.executableRevision') }}</dt><dd class="digest">{{ status.posture.systemd.executableRevision }}</dd></div>
            <div><dt>{{ t('deployment.runtimeRevision') }}</dt><dd class="digest">{{ status.posture.systemd.runtimeDirectoryRevision }}</dd></div>
            <div><dt>{{ t('deployment.resourceRevision') }}</dt><dd class="digest">{{ status.posture.systemd.resourceRevision }}</dd></div>
            <div><dt>{{ t('deployment.sandboxRevision') }}</dt><dd class="digest">{{ status.posture.systemd.sandboxRevision }}</dd></div>
            <div><dt>{{ t('deployment.brokerSocketRevision') }}</dt><dd class="digest">{{ status.posture.systemd.brokerSocketRevision }}</dd></div>
            <div><dt>{{ t('deployment.managerBootRevision') }}</dt><dd class="digest">{{ status.posture.systemd.managerBootRevision }}</dd></div>
            <div><dt>{{ t('deployment.systemdRevision') }}</dt><dd class="digest">{{ status.posture.systemd.revision }}</dd></div>
          </dl>
        </template>
      </v-card-text></v-card>

      <v-card class="mt-5"><v-card-title>{{ t('deployment.doctor') }}</v-card-title><v-card-text>
        <v-chip :color="doctor?.healthy ? 'success' : 'warning'" class="mb-3">{{ doctor?.healthy ? t('deployment.healthy') : t('deployment.attention') }}</v-chip>
		<dl class="facts-grid mb-3"><div><dt>{{ t('deployment.state') }}</dt><dd>{{ safe(doctor?.state) }}</dd></div><div><dt>{{ t('deployment.evidence') }}</dt><dd>{{ safe(doctor?.evidenceStatus) }}</dd></div></dl>
        <div class="table-scroll"><table class="status-table"><thead><tr><th>{{ t('deployment.severity') }}</th><th>{{ t('deployment.finding') }}</th><th>{{ t('deployment.nextAction') }}</th></tr></thead><tbody>
          <tr v-for="finding in doctor?.findings ?? []" :key="finding.code"><td>{{ finding.severity }}</td><td>{{ finding.code }}</td><td>{{ finding.remediation }}</td></tr>
          <tr v-if="!doctor?.findings?.length"><td colspan="3">{{ t('deployment.noFindings') }}</td></tr>
        </tbody></table></div>
      </v-card-text></v-card>

      <v-card class="mt-5"><v-card-title>{{ t('deployment.profiles') }}</v-card-title><v-card-text class="table-scroll">
        <table class="status-table"><thead><tr><th>{{ t('deployment.profile') }}</th><th>{{ t('deployment.kind') }}</th><th>{{ t('deployment.support') }}</th><th>{{ t('deployment.network') }}</th><th>{{ t('deployment.identities') }}</th><th>{{ t('deployment.capabilities') }}</th><th>{{ t('deployment.writeScopes') }}</th><th>{{ t('deployment.evidence') }}</th><th>{{ t('deployment.constraints') }}</th></tr></thead><tbody>
          <tr v-for="profile in profiles" :key="profile.id"><td>{{ profile.id }}</td><td>{{ profile.runtime }}</td><td>{{ profile.support }}</td><td>{{ profile.hostNetwork ? 'host' : profile.explicitPorts ? 'bridge-explicit' : 'native' }}</td><td>{{ reasons(profile.processIdentities) }}</td><td>{{ reasons(profile.networkCapabilities) }}</td><td>{{ reasons(profile.writeScopes) }}</td><td>{{ safe(profile.evidenceStatus) }}</td><td>{{ reasons(profile.constraints) }}</td></tr>
        </tbody></table>
      </v-card-text></v-card>

      <v-card class="mt-5"><v-card-title>{{ t('deployment.migration') }}</v-card-title><v-card-subtitle>{{ t('deployment.migrationWarning') }}</v-card-subtitle><v-card-text>
        <v-select v-model="targetProfile" :items="migrationTargets" item-title="id" item-value="id" :label="t('deployment.target')" />
        <v-checkbox v-model="acknowledged" :label="t('deployment.acknowledge')" />
        <v-btn color="primary" :loading="working" @click="runPreview">{{ t('deployment.preview') }}</v-btn>
        <template v-if="preview">
          <v-alert :type="preview.possible ? 'success' : 'warning'" variant="tonal" class="my-4">{{ preview.possible ? t('deployment.ready') : reasons(preview.reasons) }}</v-alert>
          <dl class="facts-grid mb-4"><div><dt>{{ t('deployment.source') }}</dt><dd>{{ preview.posture.profile }}</dd></div><div><dt>{{ t('deployment.target') }}</dt><dd>{{ preview.target.id }}</dd></div><div><dt>{{ t('deployment.rollbackReadiness') }}</dt><dd>{{ safe(preview.doctor.capabilities.rollback) }}</dd></div><div><dt>{{ t('deployment.previewRevision') }}</dt><dd class="digest">{{ preview.revision }}</dd></div></dl>
          <v-text-field v-model="typedConfirmation" :label="t('deployment.confirmation')" :hint="confirmationText" persistent-hint autocomplete="off" />
          <v-text-field v-model="credential" :label="t('deployment.credential')" type="password" autocomplete="current-password" />
          <v-btn color="warning" :disabled="!canMigrate" :loading="working" @click="startMigration">{{ t('deployment.start') }}</v-btn>
        </template>
      </v-card-text></v-card>

      <v-alert v-if="recovery?.required && !operation" type="error" variant="tonal" class="mt-5" role="alert">{{ t('deployment.manualRecovery') }}</v-alert>
      <v-card v-if="operation" class="mt-5"><v-card-title>{{ t('deployment.operation') }}</v-card-title><v-card-text>
        <dl class="facts-grid"><div><dt>ID</dt><dd>{{ operation.operationId }}</dd></div><div><dt>{{ t('deployment.state') }}</dt><dd>{{ operation.state }}</dd></div><div><dt>{{ t('deployment.source') }}</dt><dd>{{ operation.fromProfile }}</dd></div><div><dt>{{ t('deployment.target') }}</dt><dd>{{ operation.targetProfile }}</dd></div><div><dt>{{ t('deployment.revision') }}</dt><dd>{{ operation.revision }}</dd></div><div><dt>{{ t('deployment.rollbackReadiness') }}</dt><dd>{{ yesNo(operation.rollbackAvailable) }}</dd></div></dl>
        <v-alert v-if="operation.restoredUntrusted || operation.state === 'MANUAL_RECOVERY_REQUIRED'" type="error" variant="tonal" class="my-3">{{ t('deployment.manualRecovery') }}</v-alert>
        <v-text-field v-if="operation.state === 'VERIFYING' || operation.rollbackAvailable" v-model="operationCredential" :label="t('deployment.credential')" type="password" autocomplete="current-password" class="mt-3" />
        <div class="d-flex flex-wrap ga-2 mb-4"><v-btn v-if="operation.state === 'VERIFYING'" color="success" :disabled="!operationCredential" @click="confirmMigration">{{ t('deployment.commit') }}</v-btn><v-btn v-if="operation.rollbackAvailable" color="error" variant="outlined" :disabled="!operationCredential" @click="rollbackMigration">{{ t('deployment.rollback') }}</v-btn></div>
        <h2 class="text-h6">{{ t('deployment.timeline') }}</h2><div class="table-scroll"><table class="status-table"><thead><tr><th>#</th><th>{{ t('deployment.state') }}</th><th>{{ t('deployment.event') }}</th><th>{{ t('deployment.reasons') }}</th><th>{{ t('deployment.observed') }}</th></tr></thead><tbody><tr v-for="entry in timeline" :key="entry.sequence"><td>{{ entry.sequence }}</td><td>{{ entry.state }}</td><td>{{ entry.event }}</td><td>{{ safe(entry.reasonCode) }}</td><td>{{ formatTime(entry.createdAt) }}</td></tr></tbody></table></div>
      </v-card-text></v-card>
    </template>
    <div class="sr-only" aria-live="polite">{{ liveStatus }}</div>
  </v-container>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'
import {
  confirmDeploymentMigration, deploymentMessage, getDeploymentBroker, getDeploymentCapabilities, getDeploymentDoctor,
  getDeploymentOperation, getDeploymentProfiles, getDeploymentRecovery, getDeploymentStatus, getDeploymentTimeline, previewDeployment,
  rollbackDeploymentMigration, startDeploymentMigration, type DeploymentBroker, type DeploymentCapabilities,
  type DeploymentDoctor, type DeploymentOperation, type DeploymentPreview, type DeploymentProfile, type DeploymentRecovery,
  type DeploymentProfileID, type DeploymentStatus, type DeploymentTimeline,
} from '@/shared/composables/useDeployment'

const { t } = useI18n()
const loading = ref(true), working = ref(false)
const errorMessage = ref(''), liveStatus = ref('')
const status = ref<DeploymentStatus | null>(null), broker = ref<DeploymentBroker | null>(null)
const capabilities = ref<DeploymentCapabilities | null>(null), doctor = ref<DeploymentDoctor | null>(null)
const recovery = ref<DeploymentRecovery | null>(null)
const profiles = ref<DeploymentProfile[]>([]), preview = ref<DeploymentPreview | null>(null)
const operation = ref<DeploymentOperation | null>(null), timeline = ref<DeploymentTimeline[]>([])
const targetProfile = ref<DeploymentProfileID>('native-hardened'), acknowledged = ref(false)
const typedConfirmation = ref(''), credential = ref(''), operationCredential = ref('')
const migrationTargets = computed(() => profiles.value.filter(item => item.id === 'native-hardened' || item.id === 'native-network-advanced'))
const confirmationText = computed(() => `MIGRATE_TO_${targetProfile.value.toUpperCase().replaceAll('-', '_')}`)
const canMigrate = computed(() => Boolean(preview.value?.possible && typedConfirmation.value === confirmationText.value && credential.value))

const loadAll = async () => {
  loading.value = true; errorMessage.value = ''
  const [profilesResponse, statusResponse, doctorResponse, brokerResponse, capabilitiesResponse, recoveryResponse] = await Promise.all([
    getDeploymentProfiles(), getDeploymentStatus(), getDeploymentDoctor(), getDeploymentBroker(), getDeploymentCapabilities(), getDeploymentRecovery(),
  ])
  profiles.value = deploymentMessage<{ items: DeploymentProfile[] }>(profilesResponse)?.items ?? []
  status.value = deploymentMessage<DeploymentStatus>(statusResponse)
  doctor.value = deploymentMessage<DeploymentDoctor>(doctorResponse)
  broker.value = deploymentMessage<DeploymentBroker>(brokerResponse)
  capabilities.value = deploymentMessage<DeploymentCapabilities>(capabilitiesResponse)
	recovery.value = deploymentMessage<DeploymentRecovery>(recoveryResponse)
	if (!operation.value && recovery.value?.operation) operation.value = recovery.value.operation
	if (operation.value) await refreshOperation()
  errorMessage.value = [profilesResponse, brokerResponse, capabilitiesResponse, recoveryResponse].find(item => !item.success)?.msg ?? ''
  loading.value = false; liveStatus.value = errorMessage.value || t('deployment.refresh')
}
const runPreview = async () => {
  working.value = true; preview.value = null; typedConfirmation.value = ''; errorMessage.value = ''
  const response = await previewDeployment(targetProfile.value, acknowledged.value)
  preview.value = deploymentMessage<DeploymentPreview>(response); errorMessage.value = response.success ? '' : response.msg
  working.value = false; liveStatus.value = errorMessage.value || (preview.value?.possible ? t('deployment.ready') : t('deployment.attention'))
}
const startMigration = async () => {
  const current = preview.value; if (!current || !canMigrate.value) return
  working.value = true
  const target = `deployment-profile:${current.target.id}:${current.revision}`
  const grant = await acquireStepUpToken('deployment.migrate', target, credential.value)
  if (!grant.token) { errorMessage.value = grant.response.msg; working.value = false; return }
  const response = await startDeploymentMigration({ targetProfile: current.target.id, idempotencyKey: `deployment-ui:${crypto.randomUUID()}`,
    expectedPreviewRevision: current.revision, expectedPostureRevision: current.posture.revision,
    confirmation: typedConfirmation.value, acknowledged: true }, grant.token)
  operation.value = deploymentMessage<DeploymentOperation>(response); errorMessage.value = response.success ? '' : response.msg
  credential.value = ''; typedConfirmation.value = ''; working.value = false
  if (operation.value) await refreshOperation()
}
const refreshOperation = async () => {
  if (!operation.value) return
  const [operationResponse, timelineResponse] = await Promise.all([getDeploymentOperation(operation.value.operationId), getDeploymentTimeline(operation.value.operationId)])
  operation.value = deploymentMessage<DeploymentOperation>(operationResponse) ?? operation.value
  timeline.value = deploymentMessage<{ items: DeploymentTimeline[] }>(timelineResponse)?.items ?? []
}
const mutateOperation = async (action: 'confirm' | 'rollback') => {
  const current = operation.value; if (!current || !operationCredential.value) return
  const operationKind = action === 'confirm' ? 'deployment.confirm' : 'deployment.rollback'
  const target = `deployment-operation:${current.operationId}:${current.revision}`
  const grant = await acquireStepUpToken(operationKind, target, operationCredential.value)
  if (!grant.token) { errorMessage.value = grant.response.msg; return }
  const response = action === 'confirm'
    ? await confirmDeploymentMigration(current.operationId, current.revision, grant.token)
    : await rollbackDeploymentMigration(current.operationId, current.revision, grant.token)
  operation.value = deploymentMessage<DeploymentOperation>(response) ?? operation.value
  errorMessage.value = response.success ? '' : response.msg; operationCredential.value = ''; await refreshOperation()
}
const confirmMigration = () => mutateOperation('confirm')
const rollbackMigration = () => mutateOperation('rollback')
const safe = (value: unknown) => typeof value === 'string' && value.trim() ? value : typeof value === 'number' ? String(value) : 'UNKNOWN'
const reasons = (values?: string[]) => values?.length ? values.join(', ') : t('deployment.none')
const yesNo = (value: boolean) => value ? t('yes') : t('no')
const formatTime = (unix: number) => unix > 0 ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(unix * 1000)) : 'UNKNOWN'
onMounted(() => void loadAll())
</script>

<style scoped>
.facts-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
.facts-grid dt { font-size: .75rem; opacity: .75; }.facts-grid dd { margin: 0; overflow-wrap: anywhere; }
.digest { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: .78rem; }.table-scroll { overflow-x: auto; }
.status-table { width: 100%; border-collapse: collapse; }.status-table th,.status-table td { padding: .65rem; text-align: left; vertical-align: top; border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); overflow-wrap: anywhere; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; border: 0; }
@media (max-width: 600px) { .facts-grid { grid-template-columns: 1fr; } }
@media (prefers-reduced-motion: reduce) { *,*::before,*::after { transition-duration: .01ms !important; animation-duration: .01ms !important; } }
</style>
