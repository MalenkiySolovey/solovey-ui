import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/plugins/httputil', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}))

vi.mock('@/store/modules/data', () => ({
  default: () => ({ loadData: vi.fn(), onlines: {} }),
}))

import { WsLike, WsRuntime } from '../ws'

class FakeSocket implements WsLike {
  onopen: ((event?: any) => void) | null = null
  onmessage: ((event: any) => void) | null = null
  onclose: ((event?: any) => void) | null = null
  onerror: ((event?: any) => void) | null = null
  close = vi.fn(() => {
    this.onclose?.()
  })
}

class ManualTimers {
  private nextID = 1
  timeouts: Array<{ id: number; callback: () => void; delay?: number }> = []
  intervals: Array<{ id: number; callback: () => void; delay?: number }> = []

  setTimeout = vi.fn((handler: TimerHandler, delay?: number) => {
    const callback = typeof handler === 'function' ? handler as () => void : () => undefined
    const timer = { id: this.nextID++, callback, delay }
    this.timeouts.push(timer)
    return timer.id
  }) as unknown as typeof setTimeout

  clearTimeout = vi.fn((timerID?: number) => {
    this.timeouts = this.timeouts.filter((entry) => entry.id !== timerID)
  }) as unknown as typeof clearTimeout

  setInterval = vi.fn((handler: TimerHandler, delay?: number) => {
    const callback = typeof handler === 'function' ? handler as () => void : () => undefined
    const timer = { id: this.nextID++, callback, delay }
    this.intervals.push(timer)
    return timer.id
  }) as unknown as typeof setInterval

  clearInterval = vi.fn((timerID?: number) => {
    this.intervals = this.intervals.filter((entry) => entry.id !== timerID)
  }) as unknown as typeof clearInterval

  runTimeout(delay?: number) {
    const index = this.timeouts.findIndex((entry) => entry.delay === delay)
    const timer = index >= 0 ? this.timeouts.splice(index, 1)[0] : this.timeouts.shift()
    timer?.callback()
  }

  runNextTimeout() {
    const timer = this.timeouts.shift()
    timer?.callback()
  }

  runInterval(index = 0) {
    this.intervals[index]?.callback()
  }
}

const flushPromises = async () => {
  await Promise.resolve()
  await Promise.resolve()
}

const runtimeDeps = (overrides: Partial<ConstructorParameters<typeof WsRuntime>[0]> = {}) => ({
  getToken: vi.fn(async () => 'ws-token'),
  createSocket: vi.fn(() => new FakeSocket()),
  loadData: vi.fn(),
  location: { protocol: 'http:', host: 'panel.test' },
  baseUrl: '/',
  ...overrides,
})

const onlineEvents = () => {
  const handlers = new Map<string, Set<() => void>>()
  return {
    target: {
      addEventListener: vi.fn((type: string, handler: EventListenerOrEventListenerObject) => {
        if (typeof handler !== 'function') return
        const bucket = handlers.get(type) ?? new Set<() => void>()
        bucket.add(handler as () => void)
        handlers.set(type, bucket)
      }),
      removeEventListener: vi.fn((type: string, handler: EventListenerOrEventListenerObject) => {
        handlers.get(type)?.delete(handler as () => void)
      }),
    } as unknown as Pick<Window, 'addEventListener' | 'removeEventListener'>,
    dispatch(type: string) {
      for (const handler of handlers.get(type) ?? []) handler()
    },
  }
}

describe('WsRuntime regression anchors', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('connects on the happy path and dispatches parsed events', async () => {
    const socket = new FakeSocket()
    const onEvent = vi.fn()
    const onState = vi.fn()
    const deps = runtimeDeps({
      createSocket: vi.fn(() => socket),
      onEvent,
      onState,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    expect(deps.createSocket).toHaveBeenCalledWith('ws://panel.test/api/realtime/ws', 'ws-token')
    expect(runtime.state).toBe('reconnecting')

    socket.onopen?.()
    expect(runtime.state).toBe('connected')
    expect(onState).toHaveBeenLastCalledWith('connected')

    socket.onmessage?.({ data: '{"type":"onlines","payload":{"alice":true}}' })
    expect(onEvent).toHaveBeenCalledWith({ type: 'onlines', payload: { alice: true } })
  })

  it('coalesces concurrent connection attempts while the token is loading', async () => {
    let resolveToken!: (token: string) => void
    const token = new Promise<string>(resolve => {
      resolveToken = resolve
    })
    const deps = runtimeDeps({ getToken: vi.fn(() => token) })
    const runtime = new WsRuntime(deps)

    const first = runtime.connect()
    const second = runtime.connect()

    expect(deps.getToken).toHaveBeenCalledTimes(1)
    resolveToken('ws-token')
    await Promise.all([first, second])

    expect(deps.createSocket).toHaveBeenCalledTimes(1)
  })

  it('does not create a socket when disconnected during token loading', async () => {
    let resolveToken!: (token: string) => void
    const token = new Promise<string>(resolve => {
      resolveToken = resolve
    })
    const deps = runtimeDeps({ getToken: vi.fn(() => token) })
    const runtime = new WsRuntime(deps)

    const pending = runtime.connect()
    runtime.disconnect()
    resolveToken('ws-token')
    await pending

    expect(deps.createSocket).not.toHaveBeenCalled()
    expect(runtime.state).toBe('degraded')
  })

  it('falls back to degraded polling when no websocket token is available', async () => {
    const timers = new ManualTimers()
    const deps = runtimeDeps({
      getToken: vi.fn(async () => null),
      setInterval: timers.setInterval,
      clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()

    expect(runtime.state).toBe('degraded')
    expect(deps.createSocket).not.toHaveBeenCalled()
    expect(timers.setInterval).toHaveBeenCalledWith(expect.any(Function), 10000)

    timers.runInterval()
    expect(deps.loadData).toHaveBeenCalledTimes(1)
  })

  it('falls back when the socket does not open before the timeout', async () => {
    const timers = new ManualTimers()
    const socket = new FakeSocket()
    const deps = runtimeDeps({
      createSocket: vi.fn(() => socket),
      setTimeout: timers.setTimeout,
      clearTimeout: timers.clearTimeout,
      setInterval: timers.setInterval,
      clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    expect(runtime.state).toBe('reconnecting')

    timers.runNextTimeout()

    expect(socket.close).toHaveBeenCalledTimes(1)
    expect(runtime.state).toBe('degraded')
    expect(timers.setInterval).toHaveBeenCalledWith(expect.any(Function), 10000)
  })

  it('ignores malformed messages without closing a healthy websocket', async () => {
    const socket = new FakeSocket()
    const onEvent = vi.fn()
    const runtime = new WsRuntime(runtimeDeps({
      createSocket: vi.fn(() => socket),
      onEvent,
    }))

    await runtime.connect()
    socket.onopen?.()
    socket.onmessage?.({ data: '{not-json' })

    expect(runtime.state).toBe('connected')
    expect(socket.close).not.toHaveBeenCalled()
    expect(onEvent).not.toHaveBeenCalled()
  })

  it('schedules a reconnect after a connected socket closes once', async () => {
    const timers = new ManualTimers()
    const sockets: FakeSocket[] = []
    const deps = runtimeDeps({
      createSocket: vi.fn(() => {
        const socket = new FakeSocket()
        sockets.push(socket)
        return socket
      }),
      setTimeout: timers.setTimeout,
      clearTimeout: timers.clearTimeout,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    sockets[0].onopen?.()
    sockets[0].onclose?.({ code: 1006 })

    expect(runtime.state).toBe('reconnecting')
    expect(timers.setTimeout).toHaveBeenLastCalledWith(expect.any(Function), expect.any(Number))

    timers.runNextTimeout()
    await flushPromises()

    expect(deps.getToken).toHaveBeenCalledTimes(2)
    expect(deps.createSocket).toHaveBeenCalledTimes(2)
  })

  it('enters fallback after three consecutive closes before open', async () => {
    const timers = new ManualTimers()
    const sockets: FakeSocket[] = []
    const deps = runtimeDeps({
      createSocket: vi.fn(() => {
        const socket = new FakeSocket()
        sockets.push(socket)
        return socket
      }),
      setTimeout: timers.setTimeout,
      clearTimeout: timers.clearTimeout,
      setInterval: timers.setInterval,
      clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    sockets[0].onclose?.({ code: 1006 })
    timers.runNextTimeout()
    await flushPromises()
    sockets[1].onclose?.({ code: 1006 })
    timers.runNextTimeout()
    await flushPromises()
    sockets[2].onclose?.({ code: 1006 })

    expect(runtime.state).toBe('degraded')
    expect(timers.setInterval).toHaveBeenCalledWith(expect.any(Function), 10000)
  })

  it('heals from degraded fallback after the fallback poll interval', async () => {
    vi.useFakeTimers()
    const socket = new FakeSocket()
    const deps = runtimeDeps({
      getToken: vi.fn()
        .mockResolvedValueOnce(null)
        .mockResolvedValueOnce('ws-token'),
      createSocket: vi.fn(() => socket),
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    expect(runtime.state).toBe('degraded')
    expect(deps.getToken).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(10000)

    expect(deps.loadData).toHaveBeenCalledTimes(1)
    expect(deps.getToken).toHaveBeenCalledTimes(2)
    expect(deps.createSocket).toHaveBeenCalledTimes(1)
    expect(runtime.state).toBe('reconnecting')

    socket.onopen?.()

    expect(runtime.state).toBe('connected')
    await vi.advanceTimersByTimeAsync(10000)
    expect(deps.loadData).toHaveBeenCalledTimes(1)
    expect(deps.createSocket).toHaveBeenCalledTimes(1)
  })

  it('heals from degraded fallback when the browser reports online', async () => {
    const timers = new ManualTimers()
    const events = onlineEvents()
    const socket = new FakeSocket()
    const deps = runtimeDeps({
      getToken: vi.fn()
        .mockResolvedValueOnce(null)
        .mockResolvedValueOnce('ws-token'),
      createSocket: vi.fn(() => socket),
      onlineEvents: events.target,
      setTimeout: timers.setTimeout,
      clearTimeout: timers.clearTimeout,
      setInterval: timers.setInterval,
      clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)

    await runtime.connect()
    expect(runtime.state).toBe('degraded')
    expect(events.target.addEventListener).toHaveBeenCalledWith('online', expect.any(Function))

    events.dispatch('online')
    timers.runTimeout(100)
    await flushPromises()

    expect(deps.getToken).toHaveBeenCalledTimes(2)
    expect(deps.createSocket).toHaveBeenCalledTimes(1)
    expect(runtime.state).toBe('reconnecting')

    socket.onopen?.()

    expect(runtime.state).toBe('connected')
    expect(events.target.removeEventListener).toHaveBeenCalledWith('online', expect.any(Function))
  })

  it('fences old socket events and preserves the replacement opening timeout', async () => {
    const timers = new ManualTimers()
    const sockets: FakeSocket[] = []
    const onEvent = vi.fn()
    const deps = runtimeDeps({
      createSocket: vi.fn(() => { const s = new FakeSocket(); sockets.push(s); return s }),
      onEvent, setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
    })
    const runtime = new WsRuntime(deps)
    await runtime.connect()
    const old = { open: sockets[0].onopen!, message: sockets[0].onmessage!, close: sockets[0].onclose!, error: sockets[0].onerror! }
    old.close({ code: 1006 })
    await runtime.connect()
    expect(timers.timeouts).toHaveLength(1)
    expect(timers.timeouts[0].delay).toBe(5000)
    old.open()
    old.message({ data: '{"type":"obsolete"}' })
    old.close({ code: 1006 })
    old.error()
    expect(runtime.state).toBe('reconnecting')
    expect(onEvent).not.toHaveBeenCalled()
    expect(sockets[0].close).not.toHaveBeenCalled()
    expect(timers.timeouts).toHaveLength(1)
    expect(timers.timeouts[0].delay).toBe(5000)
    sockets[1].onopen?.()
    expect(runtime.state).toBe('connected')
    expect(timers.timeouts).toHaveLength(0)
    runtime.disconnect()
  })

  it('ignores queued opening/reconnect callbacks after socket replacement or teardown', async () => {
    const timers = new ManualTimers()
    const sockets: FakeSocket[] = []
    const deps = runtimeDeps({
      createSocket: vi.fn(() => { const s = new FakeSocket(); sockets.push(s); return s }),
      setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
      setInterval: timers.setInterval, clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)
    await runtime.connect()
    const oldTimeout = timers.timeouts[0].callback
    sockets[0].onclose?.()
    const oldReconnect = timers.timeouts[0].callback
    await runtime.connect()
    oldTimeout()
    oldReconnect()
    await flushPromises()
    expect(runtime.state).toBe('reconnecting')
    expect(sockets).toHaveLength(2)
    expect(timers.intervals).toHaveLength(0)
    expect(timers.timeouts).toHaveLength(1)
    const newTimeout = timers.timeouts[0].callback
    runtime.disconnect()
    newTimeout()
    oldReconnect()
    await flushPromises()
    expect(sockets).toHaveLength(2)
    expect(timers.timeouts).toHaveLength(0)
    expect(timers.intervals).toHaveLength(0)
    expect(runtime.state).toBe('degraded')
  })

  it('fences queued fallback and online callbacks when a primary connection succeeds or shuts down', async () => {
    const timers = new ManualTimers()
    const events = onlineEvents()
    const socket = new FakeSocket()
    const deps = runtimeDeps({
      getToken: vi.fn().mockResolvedValueOnce(null).mockResolvedValue('token'),
      createSocket: vi.fn(() => socket), onlineEvents: events.target,
      setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
      setInterval: timers.setInterval, clearInterval: timers.clearInterval,
    })
    const runtime = new WsRuntime(deps)
    await runtime.connect()
    const fallback = timers.intervals[0].callback
    events.dispatch('online')
    const online = timers.timeouts[0].callback
    await runtime.connect()
    socket.onopen?.()
    fallback()
    online()
    await flushPromises()
    expect(deps.loadData).not.toHaveBeenCalled()
    expect(deps.getToken).toHaveBeenCalledTimes(2)
    expect(timers.timeouts).toHaveLength(0)
    runtime.disconnect()
    fallback()
    online()
    await flushPromises()
    expect(deps.getToken).toHaveBeenCalledTimes(2)
    expect(timers.intervals).toHaveLength(0)
  })

  it('does not swallow a current consumer programming error as malformed JSON', async () => {
    const socket = new FakeSocket()
    const runtime = new WsRuntime(runtimeDeps({ createSocket: () => socket, onEvent: () => { throw new Error('consumer defect') } }))
    await runtime.connect()
    expect(() => socket.onmessage?.({ data: '{"type":"fixture"}' })).toThrow('consumer defect')
    runtime.disconnect()
  })
})
