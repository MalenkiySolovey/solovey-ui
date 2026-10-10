import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { sessionState, type SessionView } from './runtimeSessions'
import { useLiveSessions } from './useLiveSessions'

vi.mock('@/plugins/api', () => ({ default: { post: vi.fn() } }))
vi.mock('@/plugins/httputil', () => ({ default: { getRaw: vi.fn() } }))

const fresh = (generation = 'accepted'): SessionView => ({
  status: { generation, state: 'running', apiAvailable: true, observedAt: Date.now() }, maintenance: false,
  capabilities: { compiled: true, flowClose: true, parentClose: false },
  snapshot: { generation, observedAt: Date.now(), connections: [{ id: 'flow', kind: 'flow', status: 'active', identity: 'verified', clientId: 7, inbound: 'in', inboundType: 'anytls', outbound: 'direct', network: 'tcp', protocol: '', source: 'local', destination: 'fixture.invalid', createdAt: Date.now(), upload: 0, download: 0, parentControl: 'not_supported' }], total: 1, actualTotal: 1, unassociated: 0, limit: 100, truncated: false },
})
const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
const settle = async () => { await nextTick(); await Promise.resolve(); await nextTick() }
let scopes: EffectScope[] = []

describe('shared Classic/Nexus live session semantics', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(100000); scopes = [] })
  afterEach(() => { scopes.forEach(scope => scope.stop()); vi.useRealTimers() })

  const setup = (load: any, close = vi.fn()) => {
    const visible = ref(true), clientId = ref(7), revision = ref(0)
    const scope = effectScope(); scopes.push(scope)
    const sessions = scope.run(() => useLiveSessions(visible, clientId, revision, { load, close }))!
    return { sessions, visible, revision, scope }
  }

  it('distinguishes zero, maintenance, stopped, stale, unavailable and unknown identity', () => {
    const view = fresh()
    expect(sessionState(view, Date.now())).toBe('active')
    expect(sessionState(view, Date.now() + 10001)).toBe('stale')
    view.snapshot!.connections = []
    expect(sessionState(view, Date.now())).toBe('empty')
    view.snapshot!.unassociated = 1
    expect(sessionState(view, Date.now())).toBe('identity_unavailable')
    view.status.apiAvailable = false
    expect(sessionState(view, Date.now())).toBe('unavailable')
    view.status.state = 'stopped'
    expect(sessionState(view, Date.now())).toBe('stopped')
    view.maintenance = true
    expect(sessionState(view, Date.now())).toBe('maintenance')
  })

  it('cancels hidden requests and polling and ignores their late response', async () => {
    const pending = deferred<SessionView>()
    const load = vi.fn((_id, _signal) => pending.promise)
    const { sessions, visible } = setup(load)
    expect(sessions.state.value).toBe('loading')
    const signal = load.mock.calls[0][1] as AbortSignal
    visible.value = false; await settle()
    expect(signal.aborted).toBe(true)
    pending.resolve(fresh()); await settle()
    expect(sessions.view.value).toBeNull()
    await vi.advanceTimersByTimeAsync(15000)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('fences a late snapshot across realtime restart/reconnect invalidation', async () => {
    const old = deferred<SessionView>(), replacement = deferred<SessionView>()
    const load = vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(replacement.promise)
    const { sessions, revision } = setup(load)
    revision.value++; await settle()
    old.resolve(fresh('old')); await settle()
    expect(sessions.view.value).toBeNull()
    replacement.resolve(fresh('new')); await settle()
    expect(sessions.view.value?.snapshot?.generation).toBe('new')
    expect(sessions.canClose.value).toBe(true)
  })

  it('does not erase flows on acknowledgement and refreshes the observed state', async () => {
    const updated = deferred<SessionView>()
    const load = vi.fn().mockResolvedValueOnce(fresh()).mockReturnValueOnce(updated.promise)
    const close = vi.fn().mockResolvedValue({ generation: 'accepted', outcome: 'PARTIAL_DISCONNECT', matched: 1, closed: 1, remaining: 0, parentClosed: false, parentControl: 'not_supported' })
    const { sessions } = setup(load, close); await settle()
    const action = sessions.disconnect(); await settle()
    expect(sessions.view.value?.snapshot?.connections).toHaveLength(1)
    expect(sessions.outcome.value?.parentClosed).toBe(false)
    updated.resolve(fresh()); await action
    expect(sessions.view.value?.snapshot?.connections).toHaveLength(1)
    expect(close.mock.calls[0].slice(0, 3)).toEqual(['accepted', 7, undefined])
  })

  it('fails closed on authorization and a stale snapshot while a refresh stalls', async () => {
    const denied = setup(vi.fn().mockRejectedValue({ response: { status: 403 } }))
    await settle()
    expect(denied.sessions.state.value).toBe('unauthorized')
    expect(denied.sessions.canClose.value).toBe(false)
    const pending = deferred<SessionView>()
    const load = vi.fn().mockResolvedValueOnce(fresh()).mockReturnValue(pending.promise)
    const { sessions } = setup(load); await settle()
    await vi.advanceTimersByTimeAsync(15000)
    expect(sessions.state.value).toBe('stale')
    expect(sessions.canClose.value).toBe(false)
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('projects authenticated QUIC parents separately and submits exact accepted scopes', async () => {
    const view = fresh()
    view.capabilities.parentClose = true
    view.snapshot!.connections = []
    view.snapshot!.parents = [{ parentId: '3', inbound: 'quic', epoch: 'accepted-inbound', clientId: 7, inboundType: 'hysteria2', createdAt: Date.now(), parentControl: 'authenticated_quic' }]
    view.snapshot!.parentTotal = 1
    const updated = deferred<SessionView>()
    const load = vi.fn().mockResolvedValueOnce(view).mockReturnValueOnce(updated.promise)
    const close = vi.fn().mockResolvedValue({ generation: 'accepted', outcome: 'QUIC_PARENTS_CLOSED', matched: 0, closed: 0, remaining: 0, parentsMatched: 1, parentsClosed: 1, parentsRemaining: 0, parentClosed: true, parentControl: 'authenticated_quic' })
    const { sessions } = setup(load, close); await settle()
    expect(sessions.state.value).toBe('active')
    expect(sessions.canClose.value).toBe(false)
    expect(sessions.canCloseParents.value).toBe(true)
    const action = sessions.disconnectParents(); await settle()
    expect(close.mock.calls[0].slice(0, 3)).toEqual(['accepted', 7, undefined])
    expect(close.mock.calls[0][4]).toEqual([{ parentId: '3', inbound: 'quic', epoch: 'accepted-inbound' }])
    expect(sessions.view.value?.snapshot?.parents).toHaveLength(1)
    expect(sessions.outcome.value?.parentClosed).toBe(true)
    const empty = fresh(); empty.snapshot!.connections = []; empty.snapshot!.parents = []
    updated.resolve(empty); await action
    expect(sessions.state.value).toBe('empty')
  })

  it.each(['read-only', 'truncated', 'foreign', 'maintenance', 'stale'])('rejects %s QUIC parent controls', async reason => {
    const view = fresh()
    view.capabilities.parentClose = true
    view.snapshot!.parents = [{ parentId: '3', inbound: 'quic', epoch: 'accepted-inbound', clientId: 7, inboundType: 'tuic', createdAt: Date.now(), parentControl: 'authenticated_quic' }]
    if (reason === 'read-only') view.capabilities.parentClose = false
    if (reason === 'truncated') view.snapshot!.parentsTruncated = true
    if (reason === 'foreign') view.snapshot!.parents[0].clientId = 9
    if (reason === 'maintenance') view.maintenance = true
    if (reason === 'stale') view.snapshot!.generation = 'retired'
    const close = vi.fn()
    const { sessions } = setup(vi.fn().mockResolvedValue(view), close); await settle()
    expect(sessions.canCloseParents.value).toBe(false)
    await sessions.disconnectParents()
    expect(close).not.toHaveBeenCalled()
  })
})
