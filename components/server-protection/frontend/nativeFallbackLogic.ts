import type { NativeFallbackOperation, NativeFallbackPlan, NativeFallbackStatus } from './types'

export const knownNativeActualStates = [
  'NOT_APPLIED', 'PREPARED', 'APPLYING', 'HEALTH', 'APPLIED', 'DEGRADED',
  'ROLLING_BACK', 'ROLLED_BACK', 'ROLLBACK_FAILED', 'RECONCILE_REQUIRED',
] as const

export const normalizeNativeActual = (value?: string): string =>
  knownNativeActualStates.includes(value as typeof knownNativeActualStates[number]) ? String(value) : 'UNKNOWN'

export const knownNativeVariants = [
  'NONE', 'UNSUPPORTED', 'VLESS_REALITY_HANDSHAKE_TCP',
  'TROJAN_DEFAULT_FALLBACK_TCP', 'TROJAN_ALPN_FALLBACK_TCP',
] as const

export const normalizeNativeVariant = (value?: string): string =>
  knownNativeVariants.includes(value as typeof knownNativeVariants[number]) ? String(value) : 'UNKNOWN'

export const knownNativeApplyGates = ['DISABLED_BY_DEFAULT', 'EXPERIMENTAL', 'STABLE'] as const

export const normalizeNativeApplyGate = (value?: string): string =>
  knownNativeApplyGates.includes(value as typeof knownNativeApplyGates[number]) ? String(value) : 'UNKNOWN'

export const normalizeNativeDesired = (value?: string): string =>
  value === 'NATIVE_FALLBACK' ? value : 'UNKNOWN'

export const normalizeNativeRecoveryStatus = (value?: string): string =>
  value === 'required' || value === 'in_progress' || value === 'not_required' ? value : 'unknown'

export const nativeNextAction = (actual?: string): string => {
  switch (normalizeNativeActual(actual)) {
    case 'NOT_APPLIED':
    case 'ROLLED_BACK':
      return 'PREVIEW'
    case 'PREPARED':
      return 'APPLY'
    case 'APPLIED':
      return 'ROLLBACK'
    case 'APPLYING':
    case 'HEALTH':
    case 'ROLLING_BACK':
      return 'REFRESH'
    case 'DEGRADED':
    case 'ROLLBACK_FAILED':
    case 'RECONCILE_REQUIRED':
      return 'RECONCILE'
    default:
      return 'UNKNOWN'
  }
}

export const nativeTargetSelectionLocked = (operation?: NativeFallbackOperation): boolean =>
  ['PREPARED', 'APPLYING', 'HEALTH', 'APPLIED', 'DEGRADED', 'ROLLING_BACK', 'ROLLBACK_FAILED', 'RECONCILE_REQUIRED']
    .includes(normalizeNativeActual(operation?.actualState))

export const nativeConfirmationPhrases = (operationId: string) => ({
  apply: `APPLY NATIVE FALLBACK ${operationId}`,
  rollback: `ROLLBACK NATIVE FALLBACK ${operationId}`,
})

export const nativeActionAvailability = (
  status: NativeFallbackStatus | undefined,
  plan: NativeFallbackPlan | undefined,
  operation: NativeFallbackOperation | undefined,
  now = Date.now(),
) => {
  const exactCurrentOperation = Boolean(
    operation &&
    status?.latestOperation?.operationId === operation.operationId &&
    status.latestOperation.revision === operation.revision,
  )
  const providerCurrent = Boolean(
    status?.providerReservation?.fresh &&
    !status.blocks?.includes('provider_reservation_conflict'),
  )
  const statusActual = normalizeNativeActual(status?.actualState)
  return {
    prepare: Boolean(
      plan?.eligible &&
      normalizeNativeActual(plan.actualState) === 'NOT_APPLIED' &&
      (statusActual === 'NOT_APPLIED' || statusActual === 'ROLLED_BACK') &&
      new Date(plan.expiresAt).getTime() > now &&
      status?.applyGate === 'EXPERIMENTAL',
    ),
    apply: Boolean(
      normalizeNativeActual(operation?.actualState) === 'PREPARED' &&
      statusActual === 'PREPARED' &&
      exactCurrentOperation &&
      providerCurrent &&
      status?.target?.actionable &&
      !status.blocks?.length &&
      status?.applyGate === 'EXPERIMENTAL',
    ),
    rollback: Boolean(
      normalizeNativeActual(operation?.actualState) === 'APPLIED' &&
      statusActual === 'APPLIED' &&
      exactCurrentOperation &&
      providerCurrent &&
      !status?.blocks?.includes('target_reference_stale') &&
      status?.applyGate === 'EXPERIMENTAL',
    ),
  }
}
