import { describe, expect, it } from 'vitest'
import { receiptIdempotencyKey } from './idempotency'

describe('Receipt receipt idempotency keys', () => {
	it('encode one ten-digit issue second and a bounded portable nonce', () => {
		expect(receiptIdempotencyKey(1_788_115_473_000, 'fixture:nonce/value')).toBe(
			'sp-receipt.1788115473.fixture-nonce-value',
		)
	})

	it('rejects times outside the v1 fence contract', () => {
		expect(() => receiptIdempotencyKey(0, 'fixture-nonce')).toThrow()
	})
})
