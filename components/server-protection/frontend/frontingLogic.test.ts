import { describe, expect, it } from 'vitest'
import { canApplyFronting, canRollbackFronting, normalizeFrontingState, normalizeFrontingStrategy, validExactSNI } from './frontingLogic'

describe('fronting semantic UI logic', () => {
	it('renders unknown backend enums honestly', () => {
		expect(normalizeFrontingState('future-state')).toBe('UNKNOWN')
		expect(normalizeFrontingStrategy('future-strategy')).toBe('UNKNOWN')
	})

	it('accepts ASCII exact SNI only', () => {
		expect(validExactSNI('panel.example')).toBe(true)
		for (const value of ['*.example', 'PANEL.example', 'панель.example', 'single', 'bad..example']) expect(validExactSNI(value)).toBe(false)
	})

	it('does not infer mutation sequencing', () => {
		expect(canApplyFronting('PREPARED')).toBe(true)
		expect(canApplyFronting('NOT_APPLIED')).toBe(false)
		expect(canRollbackFronting('APPLIED')).toBe(true)
		expect(canRollbackFronting('PREPARED')).toBe(false)
	})
})
