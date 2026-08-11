import { describe, expect, it } from 'vitest'
import {
  nativeActionAvailability,
  nativeConfirmationPhrases,
  nativeNextAction,
  nativeTargetSelectionLocked,
  normalizeNativeActual,
  normalizeNativeApplyGate,
  normalizeNativeDesired,
  normalizeNativeRecoveryStatus,
  normalizeNativeVariant,
} from './nativeFallbackLogic'
import editorSource from './views/InboundProtectionEditor.vue?raw'
import statusSource from './views/InboundProtectionStatus.vue?raw'
import en from './locales/en'
import ru from './locales/ru'
import type { NativeFallbackOperation, NativeFallbackPlan, NativeFallbackStatus } from './types'

describe('native fallback operator state', () => {
  it('never coerces unknown, preview, or prepared state into applied', () => {
    expect(normalizeNativeActual('future_state')).toBe('UNKNOWN')
    expect(normalizeNativeDesired('future_state')).toBe('UNKNOWN')
    expect(normalizeNativeVariant('future_state')).toBe('UNKNOWN')
    expect(normalizeNativeApplyGate('future_state')).toBe('UNKNOWN')
    expect(normalizeNativeRecoveryStatus('future_state')).toBe('unknown')
    expect(normalizeNativeActual('NOT_APPLIED')).toBe('NOT_APPLIED')
    expect(normalizeNativeActual('PREPARED')).toBe('PREPARED')
    expect(normalizeNativeActual('PREPARED')).not.toBe('APPLIED')
    expect(normalizeNativeApplyGate('STABLE')).toBe('STABLE')
  })

  it('keeps preview, prepare, apply, and rollback as separate explicit actions', () => {
    const status = { applyGate: 'EXPERIMENTAL', actualState: 'NOT_APPLIED' } as NativeFallbackStatus
    const plan = { eligible: true, actualState: 'NOT_APPLIED', expiresAt: new Date(Date.now() + 60_000).toISOString() } as NativeFallbackPlan
    expect(nativeActionAvailability(status, plan, undefined).prepare).toBe(true)
    expect(nativeActionAvailability(status, plan, undefined).apply).toBe(false)

    const prepared = { operationId: 'operation-one', revision: 2, actualState: 'PREPARED' } as NativeFallbackOperation
    const preparedStatus = {
      ...status,
      actualState: 'PREPARED',
      latestOperation: prepared,
      target: { actionable: true },
      providerReservation: { fresh: true },
    } as NativeFallbackStatus
    expect(nativeActionAvailability(preparedStatus, plan, prepared)).toEqual({ prepare: false, apply: true, rollback: false })
    expect(nativeTargetSelectionLocked(prepared)).toBe(true)

    const staleStatus = { ...preparedStatus, blocks: ['target_reference_stale'] } as NativeFallbackStatus
    expect(nativeActionAvailability(staleStatus, plan, prepared).apply).toBe(false)

    const applied = { operationId: 'operation-one', revision: 3, actualState: 'APPLIED' } as NativeFallbackOperation
    const appliedStatus = { ...preparedStatus, actualState: 'APPLIED', latestOperation: applied } as NativeFallbackStatus
    expect(nativeActionAvailability(appliedStatus, plan, applied).rollback).toBe(true)
    expect(nativeTargetSelectionLocked({ actualState: 'ROLLED_BACK' } as NativeFallbackOperation)).toBe(false)
  })

  it('derives only bounded safe next actions from authoritative actual state', () => {
    expect(nativeNextAction('NOT_APPLIED')).toBe('PREVIEW')
    expect(nativeNextAction('PREPARED')).toBe('APPLY')
    expect(nativeNextAction('APPLIED')).toBe('ROLLBACK')
    expect(nativeNextAction('ROLLBACK_FAILED')).toBe('RECONCILE')
    expect(nativeNextAction('future_state')).toBe('UNKNOWN')
  })

  it('uses operation identity in exact apply and rollback phrases', () => {
    expect(nativeConfirmationPhrases('operation-one')).toEqual({
      apply: 'APPLY NATIVE FALLBACK operation-one',
      rollback: 'ROLLBACK NATIVE FALLBACK operation-one',
    })
  })

  it('keeps the editor semantic, explicit, and accessible', () => {
    expect(editorSource).toContain('item-value="key"')
    expect(editorSource).toContain('aria-live="polite"')
    expect(editorSource).toContain('autofocus')
    expect(editorSource).toContain('targetSelectionLocked')
    expect(editorSource).toContain('downgradeHelp')
    expect(editorSource).not.toMatch(/v-text-field[^>]+(?:host|port)/i)

    const previewAction = editorSource.slice(editorSource.indexOf('const requestPreview'), editorSource.indexOf('const prepare ='))
    expect(previewAction).toContain('nativeFallbackPreview')
    expect(previewAction).not.toContain('nativeFallbackPrepare')
    expect(previewAction).not.toContain('nativeFallbackApply')

    const prepareAction = editorSource.slice(editorSource.indexOf('const prepare ='), editorSource.indexOf('const apply ='))
    expect(prepareAction).toContain('nativeFallbackPrepare')
    expect(prepareAction).not.toContain('nativeFallbackApply')
    expect(statusSource).toContain('loading && !status')
    expect(statusSource).toContain('nextActionText')
    expect(statusSource).toContain('recoveryText')
  })

  it('keeps native fallback locale keys complete in English and Russian', () => {
    const keys = (value: object, prefix = ''): string[] => Object.entries(value).flatMap(([key, item]) => {
      const path = prefix ? `${prefix}.${key}` : key
      return item && typeof item === 'object' ? keys(item as object, path) : [path]
    })
    expect(keys(en.serverProtection.native).sort()).toEqual(keys(ru.serverProtection.native).sort())
  })
})
