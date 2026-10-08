import { describe, expect, it } from 'vitest'
import CoreHttpClients from './CoreHttpClients.vue'

describe('shared core HTTP editor', () => {
  it('opening, editing and reopening keeps false, zero, custom headers and shared references', () => {
    const owner = CoreHttpClients as any
    const data = { http_clients: [{ tag: 'chosen', engine: 'go', version: 2, headers: { empty: [] }, tls: { insecure: false }, idle_timeout: 0 }], route: { default_http_client: 'chosen' } }
    const state = { ...owner.data(), data }
    owner.methods.refreshText.call(state)
    owner.methods.applyDefinitions.call(state)
    expect(data.http_clients[0].tls.insecure).toBe(false)
    expect(data.http_clients[0].idle_timeout).toBe(0)
    expect(data.route.default_http_client).toBe('chosen')
    state.clientText = '{broken'
    owner.methods.applyDefinitions.call(state)
    expect(data.http_clients[0].tag).toBe('chosen')
    expect(state.parseError).toBeTruthy()
    owner.methods.refreshText.call(state)
    expect(JSON.parse(state.clientText)[0].headers.empty).toEqual([])
  })
  it('blocked or stale response does not erase current route and definitions', () => {
    const owner = CoreHttpClients as any
    const data = { http_clients: [{ tag: 'old' }], route: { rules: [], custom: false } }
    const state = { ...owner.data(), data, previewSource: JSON.stringify(data), preview: { blocked: true, http_clients: [{ tag: 'new' }], route: {} }, refreshText: () => {} }
    owner.methods.applyPreview.call(state)
    expect(data.http_clients[0].tag).toBe('old')
    state.preview.blocked = false
    state.previewSource = '{}'
    owner.methods.applyPreview.call(state)
    expect(data.route.custom).toBe(false)
  })
})
