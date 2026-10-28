import { iMultiplex } from "./multiplex"
import { iTls } from "./tls"
import { Dial } from "./dial"
import { Transport } from "./transport"
import type { QUICTuning } from './consumerTuning'

export const InTypes = {
  Direct: 'direct',
  Mixed: 'mixed',
  SOCKS: 'socks',
  HTTP: 'http',
  Shadowsocks: 'shadowsocks',
  VMess: 'vmess',
  Trojan: 'trojan',
  Naive: 'naive',
  Hysteria: 'hysteria',
  ShadowTLS: 'shadowtls',
  TUIC: 'tuic',
  Hysteria2: 'hysteria2',
  VLESS: 'vless',
  AnyTls: 'anytls',
  Snell: 'snell',
  Mieru: 'mieru',
  TrustTunnel: 'trusttunnel',
  SSH: 'ssh',
  MTProxy: 'mtproxy',
  Sudoku: 'sudoku',
  Bond: 'bond',
  CoreFailover: 'core-failover',
  Call: 'call',
  Tun: 'tun',
  Redirect: 'redirect',
  TProxy: 'tproxy',
}

type InType = typeof InTypes[keyof typeof InTypes]

export interface Addr {
  server: string
  server_port: number
  tls?: boolean
  insecure?: boolean
  server_name?: string
  remark?: string
}

export interface Listen {
  listen: string
  listen_port: number
  bind_interface?: string
  routing_mark?: number | string
  reuse_addr?: boolean
  netns?: string
  proxy_protocol?: boolean
  tcp_fast_open?: boolean
  tcp_multi_path?: boolean
  udp_fragment?: boolean
  udp_timeout?: string
  detour?: string
  disable_tcp_keep_alive?: boolean
  tcp_keep_alive?: string
  tcp_keep_alive_interval?: string
}

interface InboundBasics extends Listen {
  id: number
  type: InType
  tag: string
  tls_id: number
  addrs?: Addr[]
  out_json?: any
}

interface ShadowTLSHandShake extends Dial {
  server: string
  server_port: number
}

export interface Direct extends InboundBasics {
  network?: "udp" | "tcp"
  override_address?: string
  override_port?: number
}
export interface Mixed extends InboundBasics {
  set_system_proxy?: boolean
}
export interface SOCKS extends InboundBasics {}
export interface HTTP extends InboundBasics {
  set_system_proxy?: boolean
}
export interface Shadowsocks extends InboundBasics {
  method: string
  password: string
  network?: "udp" | "tcp"
  multiplex?: iMultiplex
  managed?: boolean
}
export interface VMess extends InboundBasics {
  tls: iTls
  multiplex?: iMultiplex
  transport?: Transport
}
export interface Trojan extends InboundBasics {
  tls: iTls
  fallback?: {
    server: string
    server_port: number
  }
  fallback_for_alpn?: {
    [alpn: string]: {
      server: string
      server_port: number
    }
  }
  multiplex?: iMultiplex
  transport?: Transport
}
export interface Naive extends InboundBasics {
  tls: iTls,
  quic_congestion_control?: "" | "bbr" | "bbr2" | "cubic" | "reno"
}
export interface Hysteria extends InboundBasics, QUICTuning {
  up_mbps: number
  down_mbps: number
  obfs?: string
  recv_window_conn?: number
  recv_window_client?: number
  max_conn_client?: number
  disable_mtu_discovery?: boolean
}
export interface ShadowTLS extends InboundBasics {
  version: 1|2|3
  password?: string
  handshake: ShadowTLSHandShake
  handshake_for_server_name?: {
    [server_name: string]: ShadowTLSHandShake
  }
  strict_mode?: boolean
  wildcard_sni?: string
}
export interface VLESS extends InboundBasics {
  multiplex?: iMultiplex
  transport?: Transport
  tls: iTls
}

export interface AnyTls extends InboundBasics {
  padding_scheme: string[]
  tls: iTls
}
export interface TUIC extends InboundBasics, QUICTuning {
  congestion_control: ""|"cubic"|"new_reno"|"bbr"
  auth_timeout?: string
  zero_rtt_handshake?: boolean
  heartbeat?: string
}
export interface Hysteria2 extends InboundBasics, QUICTuning {
  bbr_profile?: string
  up_mbps?: number
  down_mbps?: number
  obfs?: {
    type?: "salamander"
    password: string
  }
  ignore_client_bandwidth?: boolean
  masquerade?: string | {
    type: string
    directory?: string
    url?: string
    rewrite_host?: boolean
    status_code?: number
    headers?: Headers[]
    content?: string
  }
  brutal_debug?: boolean
}
export interface Tun extends InboundBasics {
  interface_name?: string
  address?: string[]
  mtu?: number
  endpoint_independent_nat?: boolean
  udp_timeout?: string
  stack?: string
  auto_route?: boolean
  strict_route?: boolean
  auto_redirect?: boolean
  exclude_mptcp?: boolean
  auto_redirect_reset_mark?: string | number
  auto_redirect_nfqueue?: number
  auto_redirect_iproute2_fallback_rule_index?: number
  auto_redirect_input_mark?: string | number
  auto_redirect_output_mark?: string | number
  iproute2_table_index?: number
  iproute2_rule_index?: number
  loopback_address?: string[]
  route_address?: string[]
  route_exclude_address?: string[]
  route_address_set?: string[]
  route_exclude_address_set?: string[]
  include_interface?: string[]
  exclude_interface?: string[]
  include_uid?: number[]
  include_uid_range?: string[]
  exclude_uid?: number[]
  exclude_uid_range?: string[]
  include_android_user?: number[]
  include_package?: string[]
  exclude_package?: string[]
  platform?: {
    http_proxy?: {
      enabled?: boolean
      server?: string
      server_port?: number
      bypass_domain?: string[]
      match_domain?: string[]
    }
  }
}
export interface Redirect extends InboundBasics {}
export interface TProxy extends InboundBasics {
  network?: "udp" | "tcp"
}

export interface Snell extends InboundBasics {
  version: 5 | 6
  psk: string
  obfs_mode?: 'none' | 'http' | 'tls'
  mode?: 'default' | 'unshaped' | 'unsafe-raw'
}

export interface Mieru extends InboundBasics {
  listen_ports?: string | string[]
  transport: 'TCP' | 'UDP'
  traffic_pattern?: string
  user_hint_is_mandatory?: boolean
  mtu?: number
}

export interface TrustTunnel extends InboundBasics {
  network?: string | string[]
  congestion_controller?: string
  cwnd?: number
  disable_path_mtu_discovery?: boolean
}

export interface SSHInbound extends InboundBasics {
  server_version?: string
  max_auth_tries?: number
  host_key?: unknown
  host_key_path?: string | string[]
}

export interface Sudoku extends InboundBasics {
  master_key?: unknown
  key?: string
  aead_method?: string
  padding_min?: number
  padding_max?: number
  table_type?: string
  handshake_timeout?: number
  enable_pure_downlink?: boolean
  custom_table?: string
  custom_tables?: string[]
  disable_http_mask?: boolean
  http_mask_mode?: string
  path_root?: string
  fallback?: string
}

export interface MTProxy extends InboundBasics {
  prefer_ip?: string
  concurrency?: number
  auto_update?: boolean
  domain_fronting_host?: string
  domain_fronting_port?: number
  domain_fronting_proxy_protocol?: boolean
  idle_timeout?: string
  handshake_timeout?: string
  tolerate_time_skewness?: string
  allow_fallback_on_unknown_dc?: boolean
  doppelganger_urls?: string | string[]
  doppelganger_per_raid?: number
  doppelganger_each?: string
  doppelganger_drs?: boolean
  throttle_max_connections?: number
  throttle_check_interval?: string
}

export interface InboundGroup extends InboundBasics {
  inbounds: string[]
}

export interface CallInbound extends InboundBasics {
  platform?: string
  mode?: string
  read_buffer?: number
  join_link?: unknown
  cookies?: unknown
  email?: string
  password?: unknown
}

// Create interfaces dynamically based on InTypes keys
type InterfaceMap = {
  direct: Direct
  mixed: Mixed
  socks: SOCKS
  http: HTTP
  shadowsocks: Shadowsocks
  vmess: VMess
  trojan: Trojan
  naive: Naive
  hysteria: Hysteria
  shadowtls: ShadowTLS
  tuic: TUIC
  hysteria2: Hysteria2
  vless: VLESS
  anytls: AnyTls
  snell: Snell
  mieru: Mieru
  trusttunnel: TrustTunnel
  ssh: SSHInbound
  mtproxy: MTProxy
  sudoku: Sudoku
  bond: InboundGroup
  'core-failover': InboundGroup
  call: CallInbound
  tun: Tun
  redirect: Redirect
  tproxy: TProxy
}

// Create union type from InterfaceMap
export type Inbound = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<InType, Inbound> = {
  direct: <Direct>{ type: InTypes.Direct },
  mixed: <Mixed>{ type: InTypes.Mixed },
  socks: <SOCKS>{ type: InTypes.SOCKS },
  http: <HTTP>{ type: InTypes.HTTP, tls_id: 0 },
  shadowsocks: <Shadowsocks>{ type: InTypes.Shadowsocks, method: 'none' },
  vmess: <VMess>{ type: InTypes.VMess, tls_id: 0, transport: {} },
  trojan: <Trojan>{ type: InTypes.Trojan, tls_id: 0, transport: {} },
  naive: <Naive>{ type: InTypes.Naive, tls_id: 0 },
  hysteria: <Hysteria>{ type: InTypes.Hysteria, up_mbps: 100, down_mbps: 100, tls_id: 0 },
  shadowtls: <ShadowTLS>{ type: InTypes.ShadowTLS, version: 3, handshake: {}, handshake_for_server_name: {} },
  tuic: <TUIC>{ type: InTypes.TUIC, congestion_control: "cubic", tls_id: 0 },
  hysteria2: <Hysteria2>{ type: InTypes.Hysteria2, tls_id: 0 },
  vless: <VLESS>{ type: InTypes.VLESS, tls_id: 0, transport: {} },
  anytls: <AnyTls>{ type: InTypes.AnyTls, tls_id: 0, padding_scheme: [
    "stop=8",
    "0=30-30",
    "1=100-400",
    "2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000",
    "3=9-9,500-1000",
    "4=500-1000",
    "5=500-1000",
    "6=500-1000",
    "7=500-1000"
  ]},
  snell: <Snell>{ type: InTypes.Snell, version: 6, psk: '' },
  mieru: <Mieru>{ type: InTypes.Mieru, transport: 'TCP' },
  trusttunnel: <TrustTunnel>{ type: InTypes.TrustTunnel, tls_id: 0, network: ['tcp', 'udp'], congestion_controller: 'bbr' },
  ssh: <SSHInbound>{ type: InTypes.SSH },
  mtproxy: <MTProxy>{ type: InTypes.MTProxy, prefer_ip: 'prefer-ipv4' },
  sudoku: <Sudoku>{ type: InTypes.Sudoku, aead_method: 'chacha20-poly1305', http_mask_mode: 'legacy' },
  bond: <InboundGroup>{ type: InTypes.Bond, inbounds: [] as string[] },
  'core-failover': <InboundGroup>{ type: InTypes.CoreFailover, inbounds: [] as string[] },
  call: <CallInbound>{ type: InTypes.Call, platform: 'telemost' },
  tun: <Tun>{ type: InTypes.Tun, mtu: 9000, stack: 'system', udp_timeout: '5m', auto_route: false },
  redirect: <Redirect>{ type: InTypes.Redirect },
  tproxy: <TProxy>{ type: InTypes.TProxy },
}

export function createInbound<T extends Inbound>(type: InType,json?: Partial<T>): Inbound {
  const defaultObject: Inbound = { ...defaultValues[type] ?? {}, ...(json ?? {}) }
  return defaultObject
}

export function isInboundGroup(type: string): boolean {
  return type === InTypes.Bond || type === InTypes.CoreFailover
}
