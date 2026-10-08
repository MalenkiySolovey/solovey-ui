import { describe, expect, it, vi } from 'vitest'
import HttpUtils from '@/plugins/httputil'
import { coreConfigContract, loadCoreConfigContract, previewCoreCompatibility } from './coreConfigContract'
vi.mock('@/plugins/httputil', () => ({ default: { get: vi.fn(), post: vi.fn() } }))

describe('shared editor facts routing', () => {
  it('posts nested compatibility drafts as JSON on the mounted route', async () => {
    const request = { config: { route: { rules: [] }, http_clients: [{ engine: 'go', tls: { insecure: false }, version: 2 }] }, includeHttp: true }
    vi.mocked(HttpUtils.post).mockResolvedValue({ success: true, msg: '', obj: { blocked: false } })
    await previewCoreCompatibility(request)
    expect(HttpUtils.post).toHaveBeenCalledExactlyOnceWith('api/compatibility-preview', request, { headers: { 'Content-Type': 'application/json' } })
  })
  it('loads the scoped backend route and shares its facts across editors', async () => {
    const facts = { maxRuleDepth: 64, maxRuleNodes: 4096, dnsActions: { respond: ['race'] }, directHttpClient: { engine: 'go', version: 2 } }
    vi.mocked(HttpUtils.get).mockResolvedValue({ success: true, msg: '', obj: facts })
    await loadCoreConfigContract()
    await loadCoreConfigContract()
    expect(HttpUtils.get).toHaveBeenCalledExactlyOnceWith('api/editor-contract')
    expect(coreConfigContract.value).toEqual(facts)
  })
})
