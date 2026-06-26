import { describe, expect, it } from 'vitest'

import { actionableLogLevel } from './dataLogLevel'

describe('actionableLogLevel', () => {
  it('maps core errors to error toasts', () => {
    expect(actionableLogLevel('ERROR failed to start')).toBe('error')
    expect(actionableLogLevel('fatal: core exited')).toBe('error')
  })

  it('maps warnings to warning toasts', () => {
    expect(actionableLogLevel('WARN route rule fallback')).toBe('warning')
    expect(actionableLogLevel('warning: deprecated option')).toBe('warning')
  })

  it('ignores non-actionable logs', () => {
    expect(actionableLogLevel('INFO outbound connection ok')).toBeUndefined()
    expect(actionableLogLevel('debug: tracker refreshed')).toBeUndefined()
  })
})
