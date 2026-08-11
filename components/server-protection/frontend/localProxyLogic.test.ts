import { describe, expect, it } from 'vitest'
import en from './locales/en'
import ru from './locales/ru'
import {
	localProxyCanApply, localProxyCanDisable, localProxyCanPrepare, localProxyHealthComplete,
	localProxyPlanReference, localProxyStateFor,
} from './localProxyLogic'
import type { LocalProxyPlan, LocalProxyState } from './localProxyTypes'

const planFixture = (): LocalProxyPlan => ({
	schema: 'solovey-ui/local-proxy-guard-plan/v1',
	planId: 'local-proxy-plan:fixture',
	planDigest: 'a'.repeat(64),
	createdAt: 1,
	expiresAt: 2,
	resourceId: 'core:inbound:17',
	endpointId: 'tcp:ipv4:1080',
	factRevision: 'b'.repeat(64),
	fact: {
		schema: 'solovey-ui/local-proxy-fact/v1',
		providerId: 'core',
		contributorId: 'core',
		resourceId: 'core:inbound:17',
		endpointId: 'tcp:ipv4:1080',
		inboundDatabaseId: 17,
		inboundType: 'mixed',
		configurationRevision: 'c'.repeat(64),
		effectiveRuntimeRevision: 'd'.repeat(64),
		runtimeIdentityRevision: 'runtime-owner',
		providerRevision: 'provider',
		capabilityRevision: 'capability',
		listenerObservationRevision: 'e'.repeat(64),
		ownerRevision: 'owner',
		healthRevision: 'f'.repeat(64),
		capacityRevision: '1'.repeat(64),
		factRevision: 'b'.repeat(64),
		configuredBind: '127.0.0.1',
		configuredPort: 1080,
		addressFamily: 'ipv4',
		observedBind: '127.0.0.1',
		observedPort: 1080,
		observedAddressFamily: 'ipv4',
		exposure: 'LOOPBACK',
		ownership: 'PROVIDER_MANAGED',
		listenerState: 'OBSERVED_EXACT',
		protocols: ['HTTP_CONNECT', 'HTTP_FORWARD', 'SOCKS5'],
		authentication: 'ABSENT',
		authenticationCount: 0,
		authenticationRevision: '2'.repeat(64),
		tls: 'DISABLED',
		tlsRevision: '3'.repeat(64),
		systemProxy: 'DISABLED',
		systemProxyRevision: '4'.repeat(64),
		dependentUdpAssociation: true,
		staticUdpListener: false,
		runtimeReady: true,
		healthCapabilityReady: true,
		capacityReady: true,
		managementCollision: 'no',
		recoveryPathCollision: 'no',
		observedAt: 1,
		expiresAt: 2,
	},
	actualState: 'NOT_APPLIED',
	applyGate: 'EXPERIMENTAL_ACK_REQUIRED',
	blockCodes: [],
	warningCodes: ['MIXED_ALL_PROTOCOLS_ATOMIC', 'SOCKS_UDP_ASSOCIATION_DIAGNOSTICS_ONLY'],
})

const stateFixture = (): LocalProxyState => ({
	resourceId: 'core:inbound:17',
	endpointId: 'tcp:ipv4:1080',
	actualState: 'PREPARED',
	applyGate: 'EXPERIMENTAL_ACK_REQUIRED',
	planId: 'local-proxy-plan:fixture',
	planDigest: 'a'.repeat(64),
	factRevision: 'b'.repeat(64),
	lease: { leaseId: 'lease-1', revision: 'c'.repeat(64), state: 'RESERVED', renewedAt: 1, expiresAt: 2 },
	latestOperationId: 'operation-1',
	latestOperationRevision: 2,
	health: [],
	providerGuarded: true,
	recoveryRequired: false,
	updatedAt: 1,
})

describe('local proxy guard frontend projection', () => {
	it('sends only the provider-authored semantic reference', () => {
		const reference = localProxyPlanReference(planFixture())
		expect(reference).toEqual({
			resourceId: 'core:inbound:17',
			endpointId: 'tcp:ipv4:1080',
			factRevision: 'b'.repeat(64),
		})
		for (const forbidden of ['host', 'ip', 'bind', 'port', 'url', 'domain', 'destination', 'target', 'sink', 'username', 'password', 'credentials', 'rawConfig']) {
			expect(forbidden in reference).toBe(false)
		}
	})

	it('gates prepare, apply and disable on exact server-returned state', () => {
		const plan = planFixture()
		const state = stateFixture()
		expect(localProxyCanPrepare(plan)).toBe(true)
		expect(localProxyCanPrepare({ ...plan, applyGate: 'BLOCKED', blockCodes: ['PUBLIC_NOT_SHIPPED'] })).toBe(false)
		expect(localProxyCanApply(state)).toBe(true)
		expect(localProxyCanApply({ ...state, recoveryRequired: true })).toBe(false)
		expect(localProxyCanApply({ ...state, lease: { ...state.lease, state: 'ACTIVE' } })).toBe(false)
		const applied = { ...state, actualState: 'APPLIED_EXPERIMENTAL' as const, lease: { ...state.lease, state: 'ACTIVE' } }
		expect(localProxyCanDisable(applied)).toBe(true)
		expect(localProxyCanDisable({ ...applied, actualState: 'RECOVERY_REQUIRED' })).toBe(false)
		expect(localProxyStateFor(plan, [{ ...state, endpointId: 'other' }, state])).toBe(state)
	})

	it('renders Mixed healthy only when every independently verified protocol passes', () => {
		const plan = planFixture()
		const state = {
			...stateFixture(),
			actualState: 'APPLIED_EXPERIMENTAL' as const,
			lease: { ...stateFixture().lease, state: 'ACTIVE' },
			health: plan.fact.protocols.map((protocol, index) => ({
				protocol, passed: true, positiveTransaction: true, missingAuthenticationDenied: true,
				invalidAuthenticationDenied: true, exactTarget: true, exactSink: true, generation: index + 1,
				completedUnixNano: 1, expiresUnixNano: 2, revision: `${index}`,
			})),
		}
		expect(localProxyHealthComplete(plan, state)).toBe(true)
		expect(localProxyHealthComplete(plan, { ...state, health: state.health.slice(0, 2) })).toBe(false)
		expect(localProxyHealthComplete(plan, { ...state, health: state.health.map((item, index) => index === 0 ? { ...item, exactSink: false } : item) })).toBe(false)
		expect(localProxyHealthComplete(plan, { ...state, actualState: 'DEGRADED' })).toBe(false)
	})

	it('keeps English and Russian local proxy locale surfaces complete', () => {
		const enKeys = Object.keys(en.serverProtection.localProxy).sort()
		const ruKeys = Object.keys(ru.serverProtection.localProxy).sort()
		expect(ruKeys).toEqual(enKeys)
		expect(en.serverProtection.tabs.localProxy).toBeTruthy()
		expect(ru.serverProtection.tabs.localProxy).toBeTruthy()
	})
})
