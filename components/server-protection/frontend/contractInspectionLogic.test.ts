import { describe, expect, it } from 'vitest'
import { factAge, factState } from './contractInspectionLogic'

describe('contract inspection facts', () => {
  it('keeps stale, truncated, and unknown distinct', () => {
    expect(factState({ stale: true })).toBe('stale')
    expect(factState({ truncated: true })).toBe('truncated')
    expect(factState({ reasonCodes: ['process_owner_unknown'] })).toBe('unknown')
    expect(factState({ reasonCodes: ['signal_scope_normalized_fail_closed'] })).toBe('unknown')
    expect(factState({ reasonCodes: [] })).toBe('current')
  })

  it('renders bounded age without inventing a timestamp', () => {
    expect(factAge(0, 100_000)).toBe('unknown')
    expect(factAge(90, 100_000)).toBe('10s')
  })
})
