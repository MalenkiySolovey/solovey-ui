import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { consumeRuntimeLogStream, useRuntimeControls, type ControlsView } from './useRuntimeControls'

vi.mock('@/plugins/api', () => ({ default: { post: vi.fn() } }))
vi.mock('@/plugins/httputil', () => ({ default: { getRaw: vi.fn() } }))

const fresh = (generation = 'accepted'): ControlsView => ({
  runtime: { status: { generation, state: 'running', apiAvailable: true, observedAt: Date.now() }, maintenance: false, maintenanceAvailable: true, capabilities: { compiled: true, flowClose: true, parentClose: false } },
  canWrite: true, canMaintenance: true, snapshot: { generation, observedAt: Date.now(), groups: [] },
})
const deferred = <T>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
const settle = async () => { await nextTick(); await Promise.resolve(); await nextTick() }
let scopes: EffectScope[] = []
describe('shared runtime controls and generation projection', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(100000); scopes = [] })
  afterEach(() => { scopes.forEach(scope => scope.stop()); vi.useRealTimers() })
  const setup = (load: any, action = vi.fn(), stream = vi.fn().mockResolvedValue(undefined)) => {
    const visible = ref(true), revision = ref(0), scope = effectScope(); scopes.push(scope)
    const controls = scope.run(() => useRuntimeControls(visible, revision, { load, action, stream }))!
    return { controls, visible, revision }
  }
  it('separates maintenance intent, actual failure and API failure without granting group actions', async () => {
    const view = fresh(), load = vi.fn(async () => structuredClone(view))
    const { controls } = setup(load); await settle()
    expect(controls.canAct.value).toBe(true)
    view.runtime.maintenance = true; view.runtime.status.state = 'stopped'
    await controls.refresh(); expect(controls.state.value).toBe('maintenance'); expect(controls.canAct.value).toBe(false); expect(controls.canMaintain.value).toBe(true)
    view.runtime.maintenance = false; view.runtime.status.state = 'stopped_by_error'
    await controls.refresh(); expect(controls.state.value).toBe('failed')
    view.runtime.status.state = 'running'; view.runtime.status.apiAvailable = false
    await controls.refresh(); expect(controls.state.value).toBe('unavailable'); expect(controls.canMaintain.value).toBe(true)
    view.runtime.maintenanceAvailable = false
    await controls.refresh(); expect(controls.canMaintain.value).toBe(false)
  })
  it('rejects an old load and mutation acknowledgement after a generation revision', async () => {
    const old = deferred<ControlsView>(), ack = deferred<Record<string, any>>()
    const load = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue(fresh('new'))
    const { controls, revision } = setup(load, vi.fn(() => ack.promise))
    revision.value++; await settle(); old.resolve(fresh('old')); await settle()
    expect(controls.view.value?.runtime.status.generation).toBe('new')
    const action = controls.action('select', { group: 'choice', member: 'direct' })
    load.mockResolvedValue(fresh('replacement')); revision.value++; await settle()
    ack.resolve({ outcome: 'RUNTIME_SELECTED' }); await action
    expect(controls.outcome.value).toBe(''); expect(controls.view.value?.runtime.status.generation).toBe('replacement')
  })
  it('retires hidden views and disables stale targets while a refresh is pending', async () => {
    const pending = deferred<ControlsView>(), load = vi.fn().mockResolvedValueOnce(fresh()).mockReturnValue(pending.promise)
    const { controls, visible } = setup(load); await settle()
    await vi.advanceTimersByTimeAsync(15000)
    expect(controls.state.value).toBe('stale'); expect(controls.canAct.value).toBe(false)
    visible.value = false; await settle()
    pending.resolve(fresh('late')); await settle()
    expect(controls.view.value).toBeNull()
    const calls = load.mock.calls.length; await vi.advanceTimersByTimeAsync(60000); expect(load.mock.calls.length).toBe(calls)
  })
  it('bounds the displayed log projection, discards another generation and does not reconnect', async () => {
    const stream = vi.fn(async (_generation, _signal, emit) => {
      emit({ generation: 'accepted', reset: true })
      for (let i = 0; i < 500; i++) emit({ generation: 'accepted', message: `${i}` })
      emit({ generation: 'old', message: 'must not appear' })
    })
    const { controls } = setup(vi.fn().mockResolvedValue(fresh()), vi.fn(), stream); await settle()
    await controls.startLogs()
    expect(controls.logs.value.length).toBe(400); expect(controls.logs.value[0].message).toBe('100')
    expect(controls.logs.value.at(-1)?.message).toBe('499'); expect(controls.streamState.value).toBe('closed')
    await vi.advanceTimersByTimeAsync(15000); expect(stream).toHaveBeenCalledTimes(1)
  })
})

describe('bounded runtime log transport', () => {
  it('parses split NDJSON frames and rejects an oversized event', async () => {
    const events: unknown[] = [], controller = new AbortController()
    const response = new Response('{"generation":"g","message":"hello"}\n', { headers: { 'Content-Type': 'application/x-ndjson' } })
    await consumeRuntimeLogStream(response, controller.signal, event => events.push(event))
    expect(events).toEqual([{ generation: 'g', message: 'hello' }])
    const oversized = new Response('x'.repeat(8193) + '\n', { headers: { 'Content-Type': 'application/x-ndjson' } })
    await expect(consumeRuntimeLogStream(oversized, controller.signal, () => {})).rejects.toThrow('runtime_limit_exceeded')
    await expect(consumeRuntimeLogStream(new Response('login'), controller.signal, () => {})).rejects.toThrow('unavailable')
  })
})
