<template>
  <v-card v-if="tag" variant="outlined" class="mt-3">
    <v-card-title class="text-subtitle-1">{{ t('serverProtection.inbound.statusTitle') }}</v-card-title>
    <v-card-text>
      <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-2" role="alert">
        {{ error }}
      </v-alert>
      <div v-if="loading && !status" class="text-caption text-medium-emphasis">{{ t('serverProtection.inbound.loading') }}</div>
      <v-alert v-else-if="!status" type="info" variant="tonal" density="compact">{{ t('serverProtection.inbound.statusUnknown') }}</v-alert>
      <template v-else>
        <div class="d-flex flex-wrap ga-2" aria-live="polite">
          <v-chip size="small" variant="tonal">{{ t('serverProtection.native.desired') }}: {{ desiredText(status.desiredState) }}</v-chip>
          <v-chip size="small" variant="tonal">{{ t('serverProtection.native.selected') }}: {{ variantText(status.selectedVariant) }}</v-chip>
          <v-chip size="small" :color="actualColor(status.actualState)" variant="tonal">{{ t('serverProtection.native.actual') }}: {{ actualText(status.actualState) }}</v-chip>
          <v-chip size="small" :color="gateColor(status.applyGate)" variant="tonal">{{ gateText(status.applyGate) }}</v-chip>
        </div>
        <div class="text-caption mt-2">{{ t('serverProtection.native.health') }}: {{ safe(status.target?.health.state) }} · {{ t('serverProtection.native.capacity') }}: {{ safe(status.target?.capacity.state) }}</div>
        <div class="text-caption">{{ t('serverProtection.native.reservation') }}: {{ safe(status.providerReservation?.state) }}</div>
        <div class="text-caption">{{ t('serverProtection.native.recoveryStatus') }}: {{ recoveryText(status.recoveryStatus) }}</div>
        <div class="text-caption">{{ t('serverProtection.native.nextAction') }}: {{ nextActionText(status.actualState) }}</div>
        <div v-if="status.latestOperation" class="text-caption">{{ t('serverProtection.native.operation') }}: {{ status.latestOperation.operationId }} · {{ safe(status.latestOperation.state) }}</div>
        <v-alert v-if="status.recoveryStatus === 'required'" type="error" variant="tonal" density="compact" class="mt-2">{{ t('serverProtection.native.recoveryRequired') }}</v-alert>
        <v-alert v-for="reason in status.reasonCodes || []" :key="reason" type="warning" variant="tonal" density="compact" class="mt-2">{{ reasonText(reason) }}</v-alert>
      </template>
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn icon="mdi-refresh" variant="text" :loading="loading" :aria-label="t('serverProtection.refresh')" :title="t('serverProtection.refresh')" @click="load" />
    </v-card-actions>
  </v-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InboundSlotContext } from '@/componentSystem/types'
import { protectionAPI } from '../api'
import {
  nativeNextAction,
  normalizeNativeActual,
  normalizeNativeApplyGate,
  normalizeNativeDesired,
  normalizeNativeRecoveryStatus,
  normalizeNativeVariant,
} from '../nativeFallbackLogic'
import type { NativeFallbackStatus } from '../types'

const props = defineProps<{ ctx: InboundSlotContext }>()
const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const status = ref<NativeFallbackStatus>()
const tag = computed(() => typeof props.ctx.inbound.tag === 'string' ? props.ctx.inbound.tag.trim() : '')
const load = async () => {
  if (!tag.value) return
  loading.value = true
  error.value = ''
  try {
    const page = await protectionAPI.nativeFallbackStatus({ limit: 200 })
    status.value = page.items.find(item => item.inbound.tag === tag.value)
  } catch {
    error.value = t('serverProtection.native.reasons.unknown')
  } finally {
    loading.value = false
  }
}
const safe = (value?: string) => value && /^[A-Za-z0-9_:-]{1,128}$/.test(value) ? value : t('serverProtection.native.unknown')
const desiredText = (value?: string) => normalizeNativeDesired(value) === 'UNKNOWN' ? t('serverProtection.native.unknown') : normalizeNativeDesired(value)
const variantText = (value?: string) => normalizeNativeVariant(value) === 'UNKNOWN' ? t('serverProtection.native.unknown') : normalizeNativeVariant(value)
const actualText = (value?: string) => normalizeNativeActual(value) === 'UNKNOWN' ? t('serverProtection.native.unknown') : normalizeNativeActual(value)
const gateText = (value?: string) => normalizeNativeApplyGate(value) === 'EXPERIMENTAL'
  ? t('serverProtection.native.experimental')
  : normalizeNativeApplyGate(value) === 'DISABLED_BY_DEFAULT'
    ? t('serverProtection.native.disabledByDefault')
    : normalizeNativeApplyGate(value) === 'STABLE'
      ? t('serverProtection.native.stable')
      : t('serverProtection.native.unknown')
const gateColor = (value?: string) => normalizeNativeApplyGate(value) === 'STABLE'
  ? 'success'
  : normalizeNativeApplyGate(value) === 'EXPERIMENTAL'
    ? 'warning'
    : normalizeNativeApplyGate(value) === 'DISABLED_BY_DEFAULT'
      ? 'error'
      : 'grey'
const recoveryText = (value?: string) => {
  const normalized = normalizeNativeRecoveryStatus(value)
  if (normalized === 'required') return t('serverProtection.native.recoveryStateRequired')
  if (normalized === 'in_progress') return t('serverProtection.native.recoveryInProgress')
  if (normalized === 'not_required') return t('serverProtection.native.recoveryNotRequired')
  return t('serverProtection.native.unknown')
}
const nextActionText = (actual?: string) => {
  const action = nativeNextAction(actual)
  if (action === 'PREVIEW') return t('serverProtection.native.nextPreview')
  if (action === 'APPLY') return t('serverProtection.native.nextApply')
  if (action === 'ROLLBACK') return t('serverProtection.native.nextRollback')
  if (action === 'REFRESH') return t('serverProtection.native.nextRefresh')
  if (action === 'RECONCILE') return t('serverProtection.native.nextReconcile')
  return t('serverProtection.native.unknown')
}
const reasonText = (code: string) => {
  const key = `serverProtection.native.reasons.${code}`
  const translated = t(key)
  return translated === key ? t('serverProtection.native.reasons.unknown') : translated
}
const actualColor = (value: string) => value === 'APPLIED' ? 'success' : ['PREPARED', 'APPLYING', 'HEALTH', 'ROLLING_BACK'].includes(value) ? 'warning' : ['DEGRADED', 'ROLLBACK_FAILED', 'RECONCILE_REQUIRED'].includes(value) ? 'error' : 'grey'
watch(tag, () => { status.value = undefined; void load() })
onMounted(load)
</script>
