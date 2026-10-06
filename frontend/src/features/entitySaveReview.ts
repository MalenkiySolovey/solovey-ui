import type { CapabilityEditState } from '@/types/capabilityEditors'

export type SaveReason = 'form.saveIdentity' | 'form.savePortRequired' | 'form.savePortInvalid' | 'form.saveTls' | 'form.savePending' | NonNullable<CapabilityEditState['message']>
export interface EditorSaveFacts {
  identity: unknown
  port?: unknown
  requiresPort?: boolean
  missingRequiredTls?: boolean
  pending: boolean
  capability?: CapabilityEditState
}

// Review only current editor facts. The backend still validates the complete
// entity and its references; this projection neither repairs nor normalizes it.
export function entitySaveReasons(facts: EditorSaveFacts): SaveReason[] {
  const reasons: SaveReason[] = []
  if (typeof facts.identity !== 'string' || !facts.identity.trim()) reasons.push('form.saveIdentity')
  if (facts.requiresPort) {
    if (facts.port === undefined || facts.port === null || facts.port === '') reasons.push('form.savePortRequired')
    else if (!['number', 'string'].includes(typeof facts.port) || !Number.isInteger(Number(facts.port)) || Number(facts.port) < 1 || Number(facts.port) > 65535) reasons.push('form.savePortInvalid')
  }
  if (facts.missingRequiredTls) reasons.push('form.saveTls')
  if (facts.capability && !facts.capability.allowed) reasons.push(facts.capability.message ?? 'capability.waiting')
  if (facts.pending) reasons.push('form.savePending')
  return reasons
}
