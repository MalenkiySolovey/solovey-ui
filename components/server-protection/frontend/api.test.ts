import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('@/plugins/api', () => ({
  default: mocks,
}))

import { protectionAPI } from './api'

const success = (obj: unknown = {}) => Promise.resolve({
  data: { success: true, msg: '', obj },
})

describe('Server Protection API wire format', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockImplementation(() => success())
    mocks.post.mockImplementation(() => success())
    mocks.put.mockImplementation(() => success())
  })

  it('walks every server-selected page without sending a duplicated page-size limit', async () => {
    mocks.get.mockImplementation((_path: string, options: { params: { page: number; limit?: number } }) => {
      const page = options.params.page
      return success({ items: page === 1 ? ['one', 'two'] : ['three'], page, limit: 2, total: 3 })
    })

    const result = await protectionAPI.getAllPages<string, { items: string[]; page: number; limit: number; total: number }>('/resources')

    expect(result.items).toEqual(['one', 'two', 'three'])
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.get.mock.calls.every(call => call[1].params.limit === undefined)).toBe(true)
  })

  it('deduplicates identical reads and serializes the normal component fan-out', async () => {
    let active = 0
    let maximum = 0
    mocks.get.mockImplementation(async () => {
      active += 1
      maximum = Math.max(maximum, active)
      await Promise.resolve()
      active -= 1
      return success({})
    })

    await Promise.all([
      protectionAPI.get('/status'),
      protectionAPI.get('/status'),
      protectionAPI.get('/resources'),
      protectionAPI.get('/profiles'),
    ])

    expect(mocks.get).toHaveBeenCalledTimes(3)
    expect(maximum).toBe(1)
  })

  it('rejects a silently short non-terminal catalog page', async () => {
    mocks.get.mockImplementation(() => success({ items: ['one'], page: 1, limit: 2, total: 3 }))

    await expect(protectionAPI.getAllPages<string, { items: string[]; page: number; limit: number; total: number }>('/resources'))
      .rejects.toMatchObject({ code: 'page_contract_incomplete' })
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })

  it('overrides the shared legacy form default for the firewall JSON body', async () => {
    const body = { includeGeneratedNft: true }

    await protectionAPI.post('/firewall/preview', body)

    expect(mocks.post).toHaveBeenCalledWith(
      'api/components/server-protection/firewall/preview',
      body,
      { headers: { 'Content-Type': 'application/json' } },
    )
  })

  it('applies the JSON override to typed mutations and PUT requests', async () => {
    const nativeBody = { resourceId: 'resource:one' }
    await protectionAPI.nativeFallbackPreview(nativeBody as never)
    await protectionAPI.put('/settings', { revision: 1 })

    expect(mocks.post).toHaveBeenCalledWith(
      'api/components/server-protection/native-fallback/preview',
      nativeBody,
      { headers: { 'Content-Type': 'application/json' } },
    )
    expect(mocks.put).toHaveBeenCalledWith(
      'api/components/server-protection/settings',
      { revision: 1 },
      { headers: { 'Content-Type': 'application/json' } },
    )
  })
})
