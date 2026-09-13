// Receipt keys carry their issue second so the durable replay fence can
// distinguish an expired retry from a genuinely new mutation after the
// bounded response row has been pruned.
export const receiptIdempotencyKey = (
	nowMilliseconds = Date.now(),
	nonce: string = globalThis.crypto?.randomUUID?.() ?? `ui-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`,
): string => {
	const issuedAt = Math.floor(nowMilliseconds / 1000)
	if (!Number.isSafeInteger(issuedAt) || issuedAt < 1_000_000_000 || issuedAt > 9_999_999_999) {
		throw new Error('idempotency key issue time is outside the v1 contract')
	}
	const safeNonce = nonce.replace(/[^A-Za-z0-9_-]/g, '-').padEnd(8, '0').slice(0, 80)
	return `sp-receipt.${issuedAt}.${safeNonce}`
}
