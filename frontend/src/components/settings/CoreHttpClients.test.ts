import { describe, expect, it } from 'vitest'
import CoreHttpClients from './CoreHttpClients.vue'

describe('shared core HTTP editor', () => {
  it('roundtrips the full HTTP dial contract without filtering detour or explicit defaults', () => {
    const owner = CoreHttpClients as any
    const client = {
      tag: 'complete', engine: 'go', detour: 'proxy-out', bind_interface: 'fixture-interface',
      inet4_bind_address: '127.0.0.1', inet6_bind_address: '::1', bind_address_no_port: false,
      routing_mark: 0, reuse_addr: false, connect_timeout: '0s', tcp_fast_open: false,
      tcp_multi_path: false, disable_tcp_keep_alive: false, tcp_keep_alive: '0s',
      domain_resolver: { server: 'dns-owner', strategy: 'prefer_ipv4', disable_cache: false },
      tls: { insecure: false }, custom_extension: { keep: true },
    }
    const data = { http_clients: [client], route: { default_http_client: 'complete' } }
    const state = { ...owner.data(), data }
    owner.methods.refreshText.call(state)
    owner.methods.applyDefinitions.call(state)
    expect(data.http_clients[0]).toEqual(client)
    expect(data.route.default_http_client).toBe('complete')
  })
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
