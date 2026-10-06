import { beforeEach, describe, expect, it, vi } from 'vitest'

const { http } = vi.hoisted(() => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

vi.mock('@/plugins/httputil', () => ({ default: http }))

import { createPinia, setActivePinia } from 'pinia'

import { actionableLogLevel } from './dataLogLevel'
import Data from './data'

beforeEach(() => {
  setActivePinia(createPinia())
  http.get.mockReset()
  http.post.mockReset()
})

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

describe('Data load ownership', () => {
  it('consumes recent probe health as an online projection and replaces it with an empty snapshot', async () => {
    const health = { direct: { tag: 'direct', source: 'manual', targetId: 'fixture-digest', status: 'healthy', delayMs: 8, checkedAt: 2000 } }
    http.get.mockResolvedValueOnce({ success: true, obj: { onlines: { outboundHealth: health } } })
    const store = Data()
    await store.loadData()
    expect(store.onlines.outboundHealth).toEqual(health)
    http.get.mockResolvedValueOnce({ success: true, obj: { onlines: { outboundHealth: {} } } })
    await store.loadData()
    expect(store.onlines.outboundHealth).toEqual({})
  })
  it('coalesces concurrent api/load refreshes into one request', async () => {
    let resolveRequest!: (value: any) => void
    http.get.mockReturnValue(new Promise(resolve => {
      resolveRequest = resolve
    }))
    const data = Data()

    const first = data.loadData()
    const second = data.loadData()

    expect(http.get).toHaveBeenCalledTimes(1)
    resolveRequest({
      success: true,
      msg: '',
      obj: {
        components: [],
        config: {},
        onlines: { inbound: [], outbound: [], user: [] },
      },
    })
    await Promise.all([first, second])

    expect(data.componentsLoaded).toBe(true)
    expect(http.get).toHaveBeenCalledTimes(1)
  })
})
