import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import HttpUtils from '@/plugins/httputil'
import { useIpHistory } from './useIpHistory'
vi.mock('@/plugins/httputil', () => ({ default: { get: vi.fn(), post: vi.fn() } }))

const deferred = <T>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const flush = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve() }
const response = (ip: string) => ({ success: true, msg: '', obj: [{ ip, firstSeen: 1, lastSeen: 2 }] })
const scopes: ReturnType<typeof effectScope>[] = []
const setup = () => {
  const visible = ref(true), client = ref('A'), cleared = vi.fn()
  const scope = effectScope(); scopes.push(scope)
  const history = scope.run(() => useIpHistory(() => [visible.value, client.value], cleared))!
  return { visible, client, cleared, history, scope }
}

describe('IP-history request/entity lifecycle', () => {
  afterEach(() => { scopes.splice(0).forEach(s => s.stop()); vi.clearAllMocks() })
  it('fences reverse-order client results and stale finally; aborts old transport', async () => {
    const a = deferred<any>(), b = deferred<any>()
    vi.mocked(HttpUtils.get).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const { client, history } = setup()
    const signal = vi.mocked(HttpUtils.get).mock.calls[0][2].signal as AbortSignal
    client.value = 'B'
    expect(signal.aborted).toBe(true)
    a.resolve(response('A')); await flush()
    expect(history.rows.value).toEqual([])
    expect(history.loading.value).toBe(true)
    b.resolve(response('B')); await flush()
    expect(history.rows.value[0].ip).toBe('B')
    expect(history.loading.value).toBe(false)
  })
  it('keeps B when A completes later and fences same-client refreshes', async () => {
    const a = deferred<any>(), b = deferred<any>(), refresh = deferred<any>()
    vi.mocked(HttpUtils.get).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise).mockReturnValueOnce(refresh.promise)
    const { client, history } = setup()
    client.value = 'B'
    b.resolve(response('B')); await flush()
    const pending = history.reload()
    a.resolve(response('A')); await flush()
    expect(history.rows.value[0].ip).toBe('B')
    expect(history.loading.value).toBe(true)
    refresh.resolve(response('B-new')); await pending
    expect(history.rows.value[0].ip).toBe('B-new')
  })
  it('fences close/reopen and scope teardown, including an aborted rejection', async () => {
    const a = deferred<any>(), reopened = deferred<any>()
    vi.mocked(HttpUtils.get).mockReturnValueOnce(a.promise).mockReturnValueOnce(reopened.promise)
    const { visible, history, scope } = setup()
    visible.value = false
    expect(history.loading.value).toBe(false)
    visible.value = true
    a.reject(new Error('late canceled request')); await flush()
    expect(history.loading.value).toBe(true)
    scope.stop()
    reopened.resolve(response('stale')); await flush()
    expect(history.rows.value).toEqual([])
    expect(history.loading.value).toBe(false)
  })
  it('fences a clear mutation and its notification across client changes', async () => {
    vi.mocked(HttpUtils.get).mockResolvedValue(response('A'))
    const clearing = deferred<any>()
    vi.mocked(HttpUtils.post).mockReturnValueOnce(clearing.promise)
    const { client, history, cleared } = setup()
    await flush()
    const pending = history.clearHistory()
    vi.mocked(HttpUtils.get).mockResolvedValue(response('B'))
    client.value = 'B'; await flush()
    clearing.resolve({ success: true, msg: '', obj: null }); await pending
    expect(history.rows.value[0].ip).toBe('B')
    expect(cleared).not.toHaveBeenCalled()
    expect(history.loading.value).toBe(false)
    vi.mocked(HttpUtils.post).mockResolvedValue({ success: true, msg: '', obj: null })
    await history.clearHistory()
    expect(history.rows.value).toEqual([])
    expect(cleared).toHaveBeenCalledTimes(1)
  })
  it('keeps current backend failure visible through the existing HTTP result contract', async () => {
    vi.mocked(HttpUtils.get).mockResolvedValue({ success: false, msg: 'current backend error', obj: null })
    const { history } = setup(); await flush()
    expect(history.loading.value).toBe(false)
    expect(history.rows.value).toEqual([])
  })
})
