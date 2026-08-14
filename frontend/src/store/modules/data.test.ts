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
