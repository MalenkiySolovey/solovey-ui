<template>
  <v-card v-if="tag" variant="outlined" class="mt-3">
    <v-card-title class="text-subtitle-1">{{ t('serverProtection.inbound.metadataTitle') }}</v-card-title>
    <v-card-text>
      <v-alert type="info" variant="tonal" density="compact" class="mb-3">
        {{ t('serverProtection.inbound.metadataHelp') }}
      </v-alert>
      <div v-if="resource" class="d-flex flex-wrap align-center ga-3">
        <v-chip size="small" variant="tonal">
          {{ resource.capabilities.known ? t('serverProtection.inbound.metadataAvailable') : t('serverProtection.inbound.protocolUnsupported') }}
        </v-chip>
        <v-switch
          v-if="profile"
          :model-value="profile.enabled"
          :label="t('serverProtection.inbound.metadataEnabled')"
          color="primary"
          hide-details
          :loading="metadataBusy"
          :disabled="metadataBusy"
          @update:model-value="setMetadataEnabled(Boolean($event))"
        />
        <v-btn
          v-else
          color="primary"
          variant="tonal"
          :loading="metadataBusy"
          :disabled="metadataBusy"
          @click="attachMetadata"
        >
          {{ t('serverProtection.inbound.attachMetadata') }}
        </v-btn>
      </div>
      <v-alert v-else-if="!loading" type="warning" variant="tonal" density="compact">
        {{ t('serverProtection.inbound.resourceUnknown') }}
      </v-alert>
    </v-card-text>
  </v-card>

  <v-card v-if="tag" variant="outlined" class="mt-3 native-fallback-editor">
    <v-card-title class="text-subtitle-1">{{ t('serverProtection.native.title') }}</v-card-title>
    <v-card-text>
      <div class="sr-only" aria-live="polite">{{ announcement }}</div>
      <v-alert v-if="message" :type="messageType" variant="tonal" density="compact" class="mb-3" role="status">
        {{ message }}
      </v-alert>
      <v-alert v-if="!resource && !loading" type="warning" variant="tonal" density="compact">
        {{ t('serverProtection.inbound.resourceUnknown') }}
      </v-alert>
      <template v-if="resource">
        <div class="native-fallback-editor__states" aria-label="Native fallback state">
          <v-chip size="small" variant="tonal">
            {{ t('serverProtection.native.desired') }}: {{ safeState(status?.desiredState) }}
          </v-chip>
          <v-chip size="small" variant="tonal">
            {{ t('serverProtection.native.selected') }}: {{ safeVariant(status?.selectedVariant) }}
          </v-chip>
          <v-chip size="small" :color="actualColor(status?.actualState)" variant="tonal">
            {{ t('serverProtection.native.actual') }}: {{ safeActual(status?.actualState) }}
          </v-chip>
          <v-chip size="small" :color="status?.applyGate === 'EXPERIMENTAL' ? 'warning' : 'error'" variant="tonal">
            {{ safeGate(status?.applyGate) }}
          </v-chip>
        </div>

        <v-list density="compact" class="mt-2">
          <v-list-item :title="t('serverProtection.native.capability')" :subtitle="safeState(status?.capability.status)" />
          <v-list-item :title="t('serverProtection.native.variant')" :subtitle="variantGuidance" />
          <v-list-item :title="t('serverProtection.native.managementIsolation')" :subtitle="preview?.managementIsolation.state || (status?.target?.actionable ? t('serverProtection.native.isolated') : t('serverProtection.native.unknown'))" />
        </v-list>

        <v-alert type="info" variant="tonal" density="compact" class="mb-3">
          <strong>{{ t('serverProtection.native.naturalTitle') }}</strong>
          {{ t('serverProtection.native.naturalHelp') }}
          <br>
          <strong>{{ t('serverProtection.native.forcedTitle') }}</strong>
          {{ t('serverProtection.native.forcedHelp') }}
          <br>
          {{ t('serverProtection.native.downgradeHelp') }}
        </v-alert>

        <v-select
          v-model="selectedTargetKey"
          :items="targetChoices"
          item-title="title"
          item-value="key"
          :label="t('serverProtection.native.target')"
          :hint="targetSelectionLocked ? t('serverProtection.native.targetLocked') : t('serverProtection.native.targetHint')"
          persistent-hint
          clearable
          :disabled="loading || busy || targetSelectionLocked"
          @update:model-value="invalidatePreview"
        />

        <v-card v-if="selectedTarget" variant="tonal" class="mt-3">
          <v-card-text>
            <div><strong>{{ selectedTarget.identity.providerId }} / {{ selectedTarget.identity.targetId }}</strong></div>
            <div>{{ targetModeLabel(selectedTarget.endpointMode) }}</div>
            <div>{{ t('serverProtection.native.health') }}: {{ safeState(selectedTarget.health.state) }} · {{ freshness(selectedTarget.health.fresh) }}</div>
            <div>{{ t('serverProtection.native.capacity') }}: {{ safeState(selectedTarget.capacity.state) }} · {{ selectedTarget.capacity.slotsUsed }}/{{ selectedTarget.capacity.slotsTotal }}</div>
          </v-card-text>
        </v-card>

        <div class="d-flex flex-wrap ga-2 mt-3">
          <v-btn color="primary" variant="tonal" prepend-icon="mdi-file-eye-outline"
            :loading="busyAction === 'preview'" :disabled="!canPreview" :title="previewBlockReason" @click="requestPreview">
            {{ t('serverProtection.native.preview') }}
          </v-btn>
          <v-btn color="warning" variant="tonal" prepend-icon="mdi-content-save-cog-outline"
            :disabled="!canPrepare" :title="prepareBlockReason" @click="prepareDialog = true">
            {{ t('serverProtection.native.prepare') }}
          </v-btn>
          <v-btn color="error" variant="tonal" prepend-icon="mdi-play-circle-outline"
            :disabled="!canApply" :title="applyBlockReason" @click="applyDialog = true">
            {{ t('serverProtection.native.apply') }}
          </v-btn>
          <v-btn color="warning" variant="outlined" prepend-icon="mdi-restore"
            :disabled="!canRollback" :title="rollbackBlockReason" @click="rollbackDialog = true">
            {{ t('serverProtection.native.rollback') }}
          </v-btn>
          <v-btn icon="mdi-refresh" variant="text" :aria-label="t('serverProtection.refresh')" :title="t('serverProtection.refresh')" :loading="loading" @click="load" />
        </div>

        <v-card v-if="preview" variant="outlined" class="mt-4">
          <v-card-title class="text-subtitle-2">{{ t('serverProtection.native.previewTitle') }}</v-card-title>
          <v-card-text>
            <div>{{ preview.resource.inboundTag }} · {{ safeVariant(preview.selectedVariant) }}</div>
            <div>{{ t('serverProtection.native.target') }}: {{ preview.target.reference.providerId }} / {{ preview.target.reference.targetId }}</div>
            <div>{{ t('serverProtection.native.beforeRevision') }}: {{ shortRevision(preview.corePreview.beforeConfigurationRevision) }}</div>
            <div>{{ t('serverProtection.native.afterRevision') }}: {{ shortRevision(preview.corePreview.expectedAfterRevision) }}</div>
            <div>{{ t('serverProtection.native.changedFields') }}: {{ changedFields(preview) }}</div>
            <div>{{ t('serverProtection.native.targetMode') }}: {{ previewTargetMode(preview) }}</div>
            <div>{{ t('serverProtection.native.health') }}: {{ safeState(preview.target.healthState) }} · {{ expiryFreshness(preview.target.healthExpiresAt) }}</div>
            <div>{{ t('serverProtection.native.capacity') }}: {{ safeState(preview.target.capacityState) }} · {{ preview.target.reservationSlotsUsed ?? 0 }}/{{ preview.target.reservationSlotsTotal ?? 0 }} · {{ expiryFreshness(preview.target.capacityExpiresAt) }}</div>
            <div>{{ t('serverProtection.native.expires') }}: {{ formatTime(preview.expiresAt) }}</div>
            <div>{{ t('serverProtection.native.managementIsolation') }}: {{ safeState(preview.managementIsolation.state) }}</div>
            <div>{{ t('serverProtection.native.applyGate') }}: {{ safeGate(preview.applyGate) }}</div>
            <div>{{ t('serverProtection.native.actual') }}: {{ safeActual(preview.actualState) }}</div>
            <v-alert v-for="reason in preview.blocks || []" :key="`block:${reason}`" type="error" variant="tonal" density="compact" class="mt-2">
              {{ reasonText(reason) }}
            </v-alert>
            <v-alert v-for="reason in preview.warnings || []" :key="`warning:${reason}`" type="warning" variant="tonal" density="compact" class="mt-2">
              {{ reasonText(reason) }}
            </v-alert>
          </v-card-text>
        </v-card>

        <v-card v-if="operation" variant="tonal" class="mt-3">
          <v-card-title class="text-subtitle-2">{{ t('serverProtection.native.operation') }}</v-card-title>
          <v-card-text>
            <div>{{ operation.operationId }}</div>
            <div>{{ t('serverProtection.native.actual') }}: {{ safeActual(operation.actualState) }}</div>
            <div>{{ t('serverProtection.native.operationState') }}: {{ safeState(operation.state) }}</div>
            <v-alert v-if="operation.recoveryRequired" type="error" variant="tonal" density="compact" class="mt-2">
              {{ t('serverProtection.native.recoveryRequired') }}
            </v-alert>
          </v-card-text>
        </v-card>
      </template>
    </v-card-text>
  </v-card>

  <v-dialog v-model="prepareDialog" max-width="560">
    <v-card>
      <v-card-title>{{ t('serverProtection.native.prepareConfirmTitle') }}</v-card-title>
      <v-card-text>{{ t('serverProtection.native.prepareConfirmHelp') }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="prepareDialog = false">{{ t('serverProtection.native.cancel') }}</v-btn>
        <v-btn color="warning" :loading="busyAction === 'prepare'" @click="prepare">{{ t('serverProtection.native.prepare') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="applyDialog" max-width="620">
    <v-card>
      <v-card-title>{{ t('serverProtection.native.applyConfirmTitle') }}</v-card-title>
      <v-card-text>
        <p>{{ t('serverProtection.native.typeExact', { phrase: applyPhrase }) }}</p>
        <v-text-field v-model="applyConfirmation" autofocus :label="t('serverProtection.native.confirmation')" :error-messages="applyConfirmation && applyConfirmation !== applyPhrase ? t('serverProtection.native.confirmationMismatch') : ''" @keyup.enter="apply" />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="applyDialog = false">{{ t('serverProtection.native.cancel') }}</v-btn>
        <v-btn color="error" :disabled="applyConfirmation !== applyPhrase" :loading="busyAction === 'apply'" @click="apply">{{ t('serverProtection.native.apply') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="rollbackDialog" max-width="620">
    <v-card>
      <v-card-title>{{ t('serverProtection.native.rollbackConfirmTitle') }}</v-card-title>
      <v-card-text>
        <p>{{ t('serverProtection.native.typeExact', { phrase: rollbackPhrase }) }}</p>
        <v-text-field v-model="rollbackConfirmation" autofocus :label="t('serverProtection.native.confirmation')" :error-messages="rollbackConfirmation && rollbackConfirmation !== rollbackPhrase ? t('serverProtection.native.confirmationMismatch') : ''" @keyup.enter="rollback" />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="rollbackDialog = false">{{ t('serverProtection.native.cancel') }}</v-btn>
        <v-btn color="warning" :disabled="rollbackConfirmation !== rollbackPhrase" :loading="busyAction === 'rollback'" @click="rollback">{{ t('serverProtection.native.rollback') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InboundSlotContext } from '@/componentSystem/types'
import { ProtectionAPIError, protectionAPI } from '../api'
import {
  nativeActionAvailability,
  nativeConfirmationPhrases,
  nativeTargetSelectionLocked,
  normalizeNativeActual,
  normalizeNativeApplyGate,
  normalizeNativeVariant,
} from '../nativeFallbackLogic'
import type {
  FallbackTargetV2Summary,
  Inventory,
  NativeFallbackOperation,
  NativeFallbackPlan,
  NativeFallbackStatus,
  Profile,
  ProtectableResource,
} from '../types'

interface Page<T> { items: T[] }
const props = defineProps<{ ctx: InboundSlotContext }>()
const { t } = useI18n()
const loading = ref(false)
const metadataBusy = ref(false)
const busyAction = ref('')
const message = ref('')
const announcement = ref('')
const messageType = ref<'error' | 'success'>('success')
const resources = ref<ProtectableResource[]>([])
const statuses = ref<NativeFallbackStatus[]>([])
const targets = ref<FallbackTargetV2Summary[]>([])
const profiles = ref<Profile[]>([])
const selectedTargetKey = ref<string>()
const preview = ref<NativeFallbackPlan>()
const operation = ref<NativeFallbackOperation>()
const prepareDialog = ref(false)
const applyDialog = ref(false)
const rollbackDialog = ref(false)
const applyConfirmation = ref('')
const rollbackConfirmation = ref('')

const tag = computed(() => typeof props.ctx.inbound.tag === 'string' ? props.ctx.inbound.tag.trim() : '')
const resource = computed(() => resources.value.find(item => item.inboundTag === tag.value))
const profile = computed(() => profiles.value.find(item => item.resourceId === resource.value?.id))
const status = computed(() => resource.value ? statuses.value.find(item => item.resourceId === resource.value?.id) : undefined)
const selectedTarget = computed(() => targets.value.find(target => targetKey(target) === selectedTargetKey.value))
const expectedConfigRevision = computed(() => status.value?.configurationRevision || resource.value?.capabilities.configRevision || '')
const targetChoices = computed(() => targets.value.map(target => ({
  key: targetKey(target),
  title: `${target.identity.providerId} / ${target.identity.targetId} · ${targetModeLabel(target.endpointMode)} · ${safeState(target.health.state)}`,
})))
const busy = computed(() => Boolean(busyAction.value))
const targetSelectionLocked = computed(() => nativeTargetSelectionLocked(operation.value))
const canPreview = computed(() => Boolean(resource.value && selectedTarget.value && expectedConfigRevision.value && !busy.value && !targetSelectionLocked.value && selectedTarget.value.actionable))
const actionAvailability = computed(() => nativeActionAvailability(status.value, preview.value, operation.value))
const canPrepare = computed(() => actionAvailability.value.prepare && !busy.value)
const canApply = computed(() => actionAvailability.value.apply && !busy.value)
const canRollback = computed(() => actionAvailability.value.rollback && !busy.value)
const previewBlockReason = computed(() => canPreview.value ? '' : t('serverProtection.native.previewBlocked'))
const prepareBlockReason = computed(() => canPrepare.value ? '' : t('serverProtection.native.prepareBlocked'))
const applyBlockReason = computed(() => canApply.value ? '' : t('serverProtection.native.applyBlocked'))
const rollbackBlockReason = computed(() => canRollback.value ? '' : t('serverProtection.native.rollbackBlocked'))
const applyPhrase = computed(() => operation.value ? nativeConfirmationPhrases(operation.value.operationId).apply : '')
const rollbackPhrase = computed(() => operation.value ? nativeConfirmationPhrases(operation.value.operationId).rollback : '')
const variantGuidance = computed(() => {
  const variant = preview.value?.runtime.admittedVariant || status.value?.capability.variant || status.value?.selectedVariant
  if (variant === 'VLESS_REALITY_HANDSHAKE_TCP') return t('serverProtection.native.realityGuidance')
  if (variant === 'TROJAN_DEFAULT_FALLBACK_TCP') return t('serverProtection.native.trojanDefaultGuidance')
  if (variant === 'TROJAN_ALPN_FALLBACK_TCP') return t('serverProtection.native.trojanAlpnGuidance')
  return t('serverProtection.native.unsupported')
})

const load = async () => {
  if (!tag.value) return
  loading.value = true
  message.value = ''
  try {
    const [inventory, statusPage, inspection, profilePage] = await Promise.all([
      protectionAPI.get<Inventory>('/resources'),
      protectionAPI.nativeFallbackStatus({ limit: 200 }),
      protectionAPI.nativeFallbackTargets(1, 200),
      protectionAPI.get<Page<Profile>>('/profiles', { limit: 200 }),
    ])
    resources.value = inventory.resources
    statuses.value = statusPage.items
    targets.value = inspection.targetsV2
    profiles.value = profilePage.items
    const current = statuses.value.find(item => item.resourceId === resources.value.find(item => item.inboundTag === tag.value)?.id)
    if (current?.latestOperation) operation.value = current.latestOperation
    if (current?.target) {
      const exact = targets.value.find(target => targetKey(target) === targetKey(current.target!))
      if (exact) selectedTargetKey.value = targetKey(exact)
    }
  } catch (reason) {
    showError(reason)
  } finally {
    loading.value = false
  }
}

const attachMetadata = async () => {
  if (!resource.value) return
  metadataBusy.value = true
  try {
    const created = await protectionAPI.post<Profile>('/profiles', {
      resourceId: resource.value.id,
      resourceRevision: resource.value.fingerprint,
      mode: 'metadata_only',
      enabled: true,
      defaultAction: 'record_only',
    })
    profiles.value.push(created)
    announce(t('serverProtection.inbound.metadataSaved'))
  } catch (reason) {
    showError(reason)
  } finally {
    metadataBusy.value = false
  }
}

const setMetadataEnabled = async (enabled: boolean) => {
  if (!profile.value) return
  metadataBusy.value = true
  try {
    const updated = await protectionAPI.put<Profile>(`/profiles/${profile.value.id}`, {
      mode: profile.value.mode,
      enabled,
      scoreThreshold: profile.value.scoreThreshold,
      graylistTtlSeconds: profile.value.graylistTtlSeconds,
      defaultAction: profile.value.defaultAction,
      revision: profile.value.revision,
    })
    const index = profiles.value.findIndex(item => item.id === updated.id)
    if (index >= 0) profiles.value[index] = updated
    announce(t('serverProtection.inbound.metadataSaved'))
  } catch (reason) {
    showError(reason)
  } finally {
    metadataBusy.value = false
  }
}

const invalidatePreview = () => {
  preview.value = undefined
}

const requestPreview = () => run('preview', async () => {
  if (!resource.value || !selectedTarget.value || !expectedConfigRevision.value) return
  preview.value = await protectionAPI.nativeFallbackPreview({
    resourceId: resource.value.id,
    expectedConfigRevision: expectedConfigRevision.value,
    targetReference: selectedTarget.value.reference,
  })
  announce(t('serverProtection.native.previewReady'))
})

const prepare = () => run('prepare', async () => {
  if (!preview.value) return
  const plan = preview.value
  const effectiveRevision = plan.resource.effectiveRevision
  const canonicalTargetRevision = plan.target.canonicalTargetRevision
  const providerRevision = plan.target.providerRevision
  const endpointRevision = plan.target.endpointRevision
  const publishRevision = plan.target.publishRevision
  const healthRevision = plan.target.healthRevision
  const capacityRevision = plan.target.capacityRevision
  if (!effectiveRevision || !canonicalTargetRevision || !providerRevision || !endpointRevision || !publishRevision || !healthRevision || !capacityRevision) {
    showError(new ProtectionAPIError('plan_digest_mismatch', 'plan_digest_mismatch'))
    return
  }
  prepareDialog.value = false
  operation.value = await protectionAPI.nativeFallbackPrepare({
    planId: plan.planId,
    planDigest: plan.planDigest,
    resourceId: plan.resource.resourceId,
    sourceRevision: plan.resource.sourceRevision,
    resourceRevision: plan.resource.resourceRevision,
    configurationRevision: plan.resource.configurationRevision,
    effectiveRevision,
    runtimeIdentityRevision: plan.runtime.identityRevision,
    capabilityResolverRevision: plan.runtime.capabilityResolverRevision,
    canonicalTargetRevision,
    providerRevision,
    endpointRevision,
    publishRevision,
    healthRevision,
    capacityRevision,
    targetReference: plan.target.reference,
    idempotencyKey: idempotencyKey(),
    experimentalRiskAcknowledged: true,
  })
  announce(t('serverProtection.native.preparedNotice'))
  await refreshStatus()
})

const apply = () => run('apply', async () => {
  if (!operation.value || applyConfirmation.value !== applyPhrase.value) return
  const providerReservationRevision = operation.value.providerReservationRevision
  if (!providerReservationRevision) {
    showError(new ProtectionAPIError('provider_reservation_conflict', 'provider_reservation_conflict'))
    return
  }
  applyDialog.value = false
  operation.value = await protectionAPI.nativeFallbackApply({
    operationId: operation.value.operationId,
    operationRevision: operation.value.revision,
    planDigest: operation.value.planDigest,
    providerReservationRevision,
    idempotencyKey: idempotencyKey(),
    confirmation: applyConfirmation.value,
  })
  applyConfirmation.value = ''
  announce(t('serverProtection.native.appliedNotice'))
  await refreshStatus()
})

const rollback = () => run('rollback', async () => {
  if (!operation.value || rollbackConfirmation.value !== rollbackPhrase.value) return
  const providerReservationRevision = operation.value.providerReservationRevision
  if (!providerReservationRevision) {
    showError(new ProtectionAPIError('provider_reservation_conflict', 'provider_reservation_conflict'))
    return
  }
  rollbackDialog.value = false
  operation.value = await protectionAPI.nativeFallbackRollback({
    operationId: operation.value.operationId,
    operationRevision: operation.value.revision,
    planDigest: operation.value.planDigest,
    providerReservationRevision,
    idempotencyKey: idempotencyKey(),
    confirmation: rollbackConfirmation.value,
  })
  rollbackConfirmation.value = ''
  announce(t('serverProtection.native.rolledBackNotice'))
  await refreshStatus()
})

const refreshStatus = async () => {
  if (!resource.value) return
  const page = await protectionAPI.nativeFallbackStatus({ resource_id: resource.value.id, limit: 1 })
  const current = page.items[0]
  if (current) {
    const index = statuses.value.findIndex(item => item.resourceId === current.resourceId)
    if (index >= 0) statuses.value[index] = current
    else statuses.value.push(current)
    if (current.latestOperation) operation.value = current.latestOperation
  }
}

const run = async (action: string, callback: () => Promise<void>) => {
  busyAction.value = action
  message.value = ''
  try {
    await callback()
  } catch (reason) {
    showError(reason)
  } finally {
    busyAction.value = ''
  }
}

const showError = (reason: unknown) => {
  messageType.value = 'error'
  const code = reason instanceof ProtectionAPIError ? reason.code : 'unknown_error'
  message.value = reasonText(code)
  announcement.value = message.value
}
const announce = (value: string) => {
  messageType.value = 'success'
  message.value = value
  announcement.value = value
}
const reasonText = (code: string) => {
  const key = `serverProtection.native.reasons.${code}`
  const translated = t(key)
  return translated === key ? t('serverProtection.native.reasons.unknown') : translated
}
const targetKey = (target: FallbackTargetV2Summary) => `${target.reference.providerId}\u0000${target.reference.targetId}\u0000${target.reference.endpointRevision}\u0000${target.reference.providerRevision}`
const targetModeLabel = (mode: string) => mode === 'TLS_HANDSHAKE_TARGET' ? t('serverProtection.native.tlsHandshakeTarget') : mode === 'PLAINTEXT_POST_TLS_TARGET' ? t('serverProtection.native.plaintextPostTlsTarget') : t('serverProtection.native.unknown')
const previewTargetMode = (plan: NativeFallbackPlan) => plan.selectedVariant === 'VLESS_REALITY_HANDSHAKE_TCP'
  ? targetModeLabel('TLS_HANDSHAKE_TARGET')
  : plan.selectedVariant === 'TROJAN_DEFAULT_FALLBACK_TCP' || plan.selectedVariant === 'TROJAN_ALPN_FALLBACK_TCP'
    ? targetModeLabel('PLAINTEXT_POST_TLS_TARGET')
    : t('serverProtection.native.unknown')
const safeState = (value?: string) => value && /^[A-Za-z0-9_:-]{1,128}$/.test(value) ? value : t('serverProtection.native.unknown')
const safeVariant = (value?: string) => normalizeNativeVariant(value) === 'UNKNOWN' ? t('serverProtection.native.unknown') : normalizeNativeVariant(value)
const safeActual = (value?: string) => normalizeNativeActual(value) === 'UNKNOWN' ? t('serverProtection.native.unknown') : normalizeNativeActual(value)
const safeGate = (value?: string) => normalizeNativeApplyGate(value) === 'EXPERIMENTAL'
  ? t('serverProtection.native.experimental')
  : normalizeNativeApplyGate(value) === 'DISABLED_BY_DEFAULT'
    ? t('serverProtection.native.disabledByDefault')
    : normalizeNativeApplyGate(value) === 'STABLE'
      ? t('serverProtection.native.stable')
      : t('serverProtection.native.unknown')
const actualColor = (value?: string) => value === 'APPLIED' ? 'success' : value === 'PREPARED' || value === 'APPLYING' || value === 'HEALTH' || value === 'ROLLING_BACK' ? 'warning' : value === 'ROLLBACK_FAILED' || value === 'RECONCILE_REQUIRED' || value === 'DEGRADED' ? 'error' : 'grey'
const freshness = (fresh: boolean) => fresh ? t('serverProtection.native.fresh') : t('serverProtection.native.stale')
const expiryFreshness = (expiresAt?: string) => freshness(Boolean(expiresAt && new Date(expiresAt).getTime() > Date.now()))
const shortRevision = (value?: string) => value ? `${value.slice(0, 10)}…` : t('serverProtection.native.unknown')
const formatTime = (value: string) => new Date(value).toLocaleString()
const changedFields = (plan: NativeFallbackPlan) => plan.selectedVariant === 'VLESS_REALITY_HANDSHAKE_TCP'
  ? t('serverProtection.native.changedRealityTarget')
  : plan.selectedVariant === 'TROJAN_ALPN_FALLBACK_TCP'
    ? t('serverProtection.native.changedTrojanAlpn')
    : t('serverProtection.native.changedTrojanDefault')
const idempotencyKey = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`

watch(tag, () => {
  selectedTargetKey.value = undefined
  preview.value = undefined
  operation.value = undefined
  void load()
})
onMounted(load)
</script>

<style scoped>
.native-fallback-editor__states { display: flex; flex-wrap: wrap; gap: .5rem; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@media (max-width: 600px) {
  .native-fallback-editor :deep(.v-btn) { width: 100%; }
}
</style>
