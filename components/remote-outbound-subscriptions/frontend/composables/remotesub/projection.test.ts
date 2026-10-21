import { describe, expect, it } from 'vitest'
import { projectionBlocked } from './projection'

describe('endpoint projection state', () => {
  it('keeps preserved critical source failures visible and unavailable for activation checks', () => {
    expect(projectionBlocked({ projectionStatus: 'MANUAL_MAPPING_REQUIRED' })).toBe(true)
    expect(projectionBlocked({ projectionStatus: 'UNSUPPORTED' })).toBe(true)
    expect(projectionBlocked({ projectionStatus: 'EXACT' })).toBe(false)
    expect(projectionBlocked({ projectionStatus: 'SUPPORTED_WITH_APPROVED_ADAPTATION' })).toBe(false)
    expect(projectionBlocked({})).toBe(false)
  })
})
