export const TrspTypes = {
  HTTP: 'http',
  WebSocket: 'ws',
  QUIC: 'quic',
  gRPC: 'grpc',
  HTTPUpgrade: "httpupgrade",
  XHTTP: 'xhttp'
}

export type TrspType = typeof TrspTypes[keyof typeof TrspTypes]

export type Transport = HTTP|WebSocket|QUIC|gRPC|HTTPUpgrade|XHTTP

interface TransportBasics {
  type: TrspType
}

export interface HTTP extends TransportBasics {
  host?: string[]
  path?: string
  method?: string
  headers?: {}
  idle_timeout?: string
  ping_timeout?: string
}

export interface WebSocket extends TransportBasics {
  path: string
  headers?: {
    Host: string
  }
  max_early_data?: number
  early_data_header_name?: string
}

export interface QUIC extends TransportBasics {}

export interface gRPC extends TransportBasics {
  service_name?: string
  idle_timeout?: string
  ping_timeout?: string
  permit_without_stream?: boolean
}

export interface HTTPUpgrade extends TransportBasics {
  host?: string
  path?: string
  headers?: {}
}

export type XHTTPRange = string | number
export interface XHTTPBase {
  host?: string
  path?: string
  headers?: Record<string, string>
  domain_strategy?: string
  x_padding_bytes: XHTTPRange
  no_grpc_header?: boolean
  no_sse_header?: boolean
  sc_max_each_post_bytes?: XHTTPRange
  sc_min_posts_interval_ms?: XHTTPRange
  sc_max_buffered_posts?: number
  sc_stream_up_server_secs?: XHTTPRange
  server_max_header_bytes?: number
  trusted_x_forwarded_for?: string[]
  xmux?: Partial<Record<'max_concurrency' | 'max_connections' | 'c_max_reuse_times' | 'h_max_request_times' | 'h_max_reusable_secs', XHTTPRange>> & { h_keep_alive_period?: number }
  x_padding_obfs_mode?: boolean
  x_padding_key?: string
  x_padding_header?: string
  x_padding_placement?: string
  x_padding_method?: string
  uplink_http_method?: string
  session_placement?: string
  session_key?: string
  seq_placement?: string
  seq_key?: string
  uplink_data_placement?: string
  uplink_data_key?: string
  uplink_chunk_size?: XHTTPRange
}
export interface XHTTP extends TransportBasics, XHTTPBase {
  mode?: string
  download?: XHTTPBase & { server?: string; server_port?: number; detour?: string; tls?: import('./tls').oTls }
}
