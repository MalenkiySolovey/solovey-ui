import { oTls } from "./tls"
import { oMultiplex } from "./multiplex"
import { Transport } from "./transport"
import { Dial } from "./dial"
import type { MemoryQuantity, QUICTuning } from './consumerTuning'

export const OutTypes = {
  Direct: 'direct',
  SOCKS: 'socks',
  HTTP: 'http',
  Shadowsocks: 'shadowsocks',
  VMess: 'vmess',
  Trojan: 'trojan',
  Naive: 'naive',
  Hysteria: 'hysteria',
  VLESS: 'vless',
  ShadowTLS: 'shadowtls',
  TUIC: 'tuic',
  Hysteria2: 'hysteria2',
  AnyTls: 'anytls',
  Snell: 'snell',
  Tor: 'tor',
  SSH: 'ssh',
  Call: 'call',
  Mieru: 'mieru',
  TrustTunnel: 'trusttunnel',
  MASQUE: 'masque',
  Bond: 'bond',
  CoreFailover: 'core-failover',
  Sudoku: 'sudoku',
  Selector: 'selector',
  URLTest: 'urltest',
	Failover: 'failover',
}

type OutType = typeof OutTypes[keyof typeof OutTypes]

interface OutboundBasics {
  id: number
  type: OutType
  tag: string
}

export interface WgPeer {
  server: string
  server_port: number
  public_key: string
  pre_shared_key?: string
  allowed_ips?: string[]
  reserved?: number[]
}

export interface Direct extends OutboundBasics, Dial {}

export interface Call extends OutboundBasics, Dial {
  platform?: string
  mode?: string
  read_buffer?: number
  join_link?: unknown
  cookies?: unknown
}

export interface Mieru extends OutboundBasics, Dial {
  server: string
  server_port?: number
  server_ports?: string | string[]
  transport?: 'TCP' | 'UDP'
  username?: string
  password?: unknown
  multiplexing?: string
  traffic_pattern?: string
  mtu?: number
  handshake_mode?: string
}

export interface Sudoku extends OutboundBasics, Dial {
  server: string
  server_port?: number
  key?: unknown
  aead_method?: string
  padding_min?: number
  padding_max?: number
  table_type?: string
  enable_pure_downlink?: boolean
  custom_table?: string
  custom_tables?: string[]
  multiplex?: string
  http_mask?: { enabled?: boolean; mode?: string; host?: string; path_root?: string; multiplex?: string; tls?: oTls }
}

export interface TrustTunnel extends OutboundBasics, Dial {
  server: string
  server_port?: number
  tls?: oTls
  username?: string
  password?: unknown
  network?: string | string[]
  health_check?: boolean
  quic?: boolean
  congestion_controller?: string
  cwnd?: number
  disable_path_mtu_discovery?: boolean
  multiplex?: { enabled?: boolean; max_connections?: number; min_streams?: number; max_streams?: number }
}

export interface SOCKS extends OutboundBasics, Dial {
  server: string
  server_port: number
  version?: "4" | "4a" | "5"
  username?: string
  password?: string
  network?: "udp" | "tcp"
  udp_over_tcp?: false | {
    enabled: true
    version?: number
  }
}

export interface MASQUE extends OutboundBasics, Dial {
  system?: boolean
  name?: string
  allowed_ips?: string | string[]
  use_http2?: boolean
  use_l4_proxy?: boolean
  use_ipv6?: boolean
  profile: { id?: string; auth_token?: unknown; private_key?: unknown; license_key?: unknown; recreate?: boolean; detour?: string }
  address?: string
  port?: number
  udp_timeout?: string
  udp_keepalive_period?: string
  udp_initial_packet_size?: number
  reconnect_delay?: string
  handshake_timeout?: string
  congestion_controller?: string
  cwnd?: number
  tls?: { server_name?: string; insecure?: boolean; cipher_suites?: string[]; curve_preferences?: string[]; fragment?: boolean; fragment_fallback_delay?: string; record_fragment?: boolean; kernel_tx?: boolean; kernel_rx?: boolean }
}

export interface HTTP extends OutboundBasics, Dial {
  server: string
  server_port: number
  username?: string
  password?: string
  path?: string
  headers?: {
    [key: string]: string
  }
  tls?: oTls
}

export interface Shadowsocks extends OutboundBasics, Dial {
  server: string
  server_port: number
  method: string
  password: string
  plugin?: string
  plugin_opts?: string
  network?: "udp" | "tcp"
  udp_over_tcp?: false | {
    enabled: true
    version?: number
  }
  multiplex?: oMultiplex
}

export interface VMESS extends OutboundBasics, Dial {
  server: string
  server_port: number
  uuid: string
  security?: string
  alter_id: 0
  global_padding?: boolean
  authenticated_length?: boolean
  network?: "udp" | "tcp"
  packet_encoding?: string
  tls?: oTls
  multiplex?: oMultiplex
  transport?: Transport
}

export interface Trojan extends OutboundBasics, Dial {
  server: string
  server_port: number
  password: string
  network?: "udp" | "tcp"
  tls?: oTls
  multiplex?: oMultiplex
  transport?: Transport
}

export interface Naive extends OutboundBasics, Dial {
  server: string
  server_port: number
  username?: string
  password?: string
  insecure_concurrency?: number
  extra_headers?: { [key: string]: string }
  udp_over_tcp?: false | { enabled?: boolean; version?: number }
  stream_receive_window?: MemoryQuantity
  quic?: boolean
  quic_congestion_control?: "" | "bbr" | "bbr2" | "cubic" | "reno"
  quic_session_receive_window?: MemoryQuantity
  tls: oTls
}

export interface Hysteria extends OutboundBasics, Dial, QUICTuning {
  server: string
  server_port: number
  server_ports?: string[]
  hop_interval?: string
  up?: string
  down?: string
  up_mbps: number
  down_mbps: number
  obfs?: string
  auth_str?: string
  recv_window_conn?: number
  recv_window?: number
  disable_mtu_discovery?: boolean
  network?: "udp" | "tcp"
  tls: oTls
}

export interface ShadowTLS extends OutboundBasics, Dial {
  server: string
  server_port: number
  version: 1|2|3
  password?: string
  tls: oTls
}

export interface VLESS extends OutboundBasics, Dial {
  server: string
  server_port: number
  uuid: string
  flow?: string
  network?: "udp" | "tcp"
  packet_encoding?: string
  tls?: oTls
  multiplex?: oMultiplex
  transport?: Transport
}

export interface TUIC extends OutboundBasics, Dial, QUICTuning {
  server: string
  server_port: number
  uuid: string
  password?: string
  congestion_control?: "cubic"|"new_reno"|"bbr"
  udp_relay_mode?: "native" | "quic"
  udp_over_stream?: boolean
  zero_rtt_handshake?: boolean
  heartbeat?: string
  network?: "udp" | "tcp"
  tls: oTls
}

export interface Hysteria2Realm {
  server_url: string
  token?: unknown
  realm_id: string
  stun_servers?: string[]
  ip_version?: 0 | 4 | 6
  port_mapping?: { enabled?: boolean; timeout?: string; lifetime?: string }
}

export interface Hysteria2 extends OutboundBasics, Dial, QUICTuning {
  realm?: Hysteria2Realm
  bbr_profile?: string
  hop_interval_max?: string
  disable_chrome_parrot?: boolean
  server: string
  server_port: number
  server_ports?: string[]
  hop_interval: string
  up_mbps?: number
  down_mbps?: number
  obfs?: {
    type?: "salamander"
    password: string
  }
  password?: string
  network?: "udp" | "tcp"
  tls: oTls
  brutal_debug?: boolean
}

export interface AnyTls extends OutboundBasics, Dial {
  server: string
  server_port: number
  password: string
  idle_session_check_interval: string
  idle_session_timeout: string
  min_idle_session: number
  tls: oTls
}

export interface Tor extends OutboundBasics, Dial {
  executable_path?: string
  extra_args?: string[]
  data_directory: string
  torrc?: {
    [options: string]: string
  }
}

export interface SSH extends OutboundBasics, Dial  {
  server: string
  server_port?: number
  user?: string
  password?: string
  private_key?: string
  private_key_path?: string
  private_key_passphrase?: string
  host_key?: string[]
  host_key_algorithms?: string[]
  client_version?: string
  cipher?: string[]
  kex_algorithm?: string[]
  mac?: string[]
}

export interface Selector extends OutboundBasics {
  outbounds: string[]
  url?: string
  interval?: string
  tolerance?: number
  idle_timeout?: string
  interrupt_exist_connections?: boolean
}

export interface URLTest extends OutboundBasics {
  outbounds: string[]
  default?: string
  interrupt_exist_connections?: boolean
}

export interface Failover extends OutboundBasics {
  outbounds: string[]
  interrupt_exist_connections?: boolean
  failover: {
    enabled?: boolean
    probe_target?: string
    interval?: string
    hysteresis?: number
  }
}

// Create interfaces dynamically based on OutTypes keys
type InterfaceMap = {
  [Key in keyof typeof OutTypes]: {
    type: string
    [otherProperties: string]: any // You can add other properties as needed
  }
}

// Create union type from InterfaceMap
export type Outbound = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<OutType, Outbound> = {
  direct: { type: OutTypes.Direct },
  socks: { type: OutTypes.SOCKS, version: "5" },
  http: { type: OutTypes.HTTP, tls: {} },
  shadowsocks: { type: OutTypes.Shadowsocks, method: 'none', multiplex: {} },
  vmess: { type: OutTypes.VMess, tls: {}, multiplex: {}, transport: {}, security: 'auto', global_padding: false },
  trojan: { type: OutTypes.Trojan, tls: {}, multiplex: {}, transport: {} },
  naive: { type: OutTypes.Naive, tls: { enabled: true } },
  hysteria: { type: OutTypes.Hysteria, up_mbps: 100, down_mbps: 100, tls: { enabled: true } },
  shadowtls: { type: OutTypes.ShadowTLS, version: 3, tls: { enabled: true } },
  vless: { type: OutTypes.VLESS, tls: {}, multiplex: {}, transport: {} },
  tuic: { type: OutTypes.TUIC, congestion_control: 'cubic', tls: { enabled: true } },
  hysteria2: { type: OutTypes.Hysteria2, tls: { enabled: true } },
  anytls: { type: OutTypes.AnyTls, tls: { enabled: true } },
  snell: { type: OutTypes.Snell, version: 6, psk: '' },
  tor: { type: OutTypes.Tor, executable_path: './tor', data_directory: '$HOME/.cache/tor', torrc: { ClientOnly: '1' } },
  ssh: { type: OutTypes.SSH },
  call: { type: OutTypes.Call, platform: 'telemost' },
  mieru: { type: OutTypes.Mieru, transport: 'TCP' },
  trusttunnel: { type: OutTypes.TrustTunnel, tls: { enabled: true } },
  masque: { type: OutTypes.MASQUE, profile: {}, tls: {} },
  bond: { type: OutTypes.Bond, outbounds: [] },
  'core-failover': { type: OutTypes.CoreFailover, outbounds: [], strategy: 'sequential' },
  sudoku: { type: OutTypes.Sudoku },
  selector: { type: OutTypes.Selector },
  urltest: { type: OutTypes.URLTest },
	failover: { type: OutTypes.Failover, outbounds: [], failover: { enabled: true, probe_target: '', interval: '30s', hysteresis: 2 } },
}

export function createOutbound<T extends Outbound>(type: string,json?: Partial<T>): Outbound {
  const defaultObject: Outbound = { ...JSON.parse(JSON.stringify(defaultValues[type] ?? {})), ...(json || {}) }
  return defaultObject
}

// Review the editable native group shape without copying member options.
// Existence, provider eligibility and cycles remain backend owner checks.
export function nativeOutboundGroupValid(data: Outbound): boolean {
  if (![OutTypes.Bond, OutTypes.CoreFailover].includes(data.type)) return true
  if (!Array.isArray(data.outbounds) || data.outbounds.length === 0) return false
  if (data.type === OutTypes.CoreFailover) return data.outbounds.every((tag: unknown) => typeof tag === 'string' && tag.trim().length > 0)
  let down = 0, up = 0, legs = 0
  for (const member of data.outbounds) {
    if (!member || typeof member.outbound !== 'string' || !member.outbound.trim()) return false
    const count = member.count ?? 0
    if (![count, member.download_ratio, member.upload_ratio].every(Number.isInteger) || count < 0 || count > 255 ||
      member.download_ratio < 0 || member.download_ratio > 100 || member.upload_ratio < 0 || member.upload_ratio > 100) return false
    legs += Math.max(count, 1)
    down += Math.max(count, 1) * member.download_ratio
    up += Math.max(count, 1) * member.upload_ratio
  }
  return legs <= 255 && down === 100 && up === 100
}
