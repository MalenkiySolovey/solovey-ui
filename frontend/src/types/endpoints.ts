import { Dial } from "./dial"

export const EpTypes = {
  Wireguard: 'wireguard',
  Warp: 'warp',
  Tailscale: 'tailscale',
  OpenConnect: 'openconnect',
  OpenVPNClient: 'openvpn-client',
  OpenVPNServer: 'openvpn-server',
}

type EpType = typeof EpTypes[keyof typeof EpTypes]

interface EndpointBasics {
  id: number
  type: EpType
  tag: string
}

export interface WgPeer {
  address: string
  port: number
  public_key: string
  pre_shared_key?: string
  allowed_ips?: string[]
  persistent_keepalive_interval?: number
  reserved?: number[]
}

export interface WireGuardAmnezia {
  jc?: number
  jmin?: number
  jmax?: number
  s1?: number
  s2?: number
  s3?: number
  s4?: number
  h1?: number | string
  h2?: number | string
  h3?: number | string
  h4?: number | string
  i1?: string
  i2?: string
  i3?: string
  i4?: string
  i5?: string
  header_protection_key?: unknown
  content_padding_addition?: number | string
  rekey_after_time?: number | string
  rekey_timeout?: number | string
  reject_after_time?: number | string
  keepalive_timeout?: number | string
  max_handshake_attempts?: number | string
  random_trailers?: boolean
  disable_cookies?: boolean
}

export interface WireGuard extends EndpointBasics, Dial {
  system?: boolean
  name?: string
  mtu?: number
  address: string[]
  private_key: string
  listen_port: number
  peers: WgPeer[]
  udp_timeout?: string
  workers?: number
  udp_mapping?: '' | 'endpoint_independent' | 'address_dependent' | 'address_and_port_dependent'
  udp_filtering?: '' | 'endpoint_independent' | 'address_dependent' | 'address_and_port_dependent'
  udp_nat_max?: number
  disable_pauses?: boolean
  preallocated_buffers_per_pool?: number
  amnezia?: WireGuardAmnezia
  ext: any
}

export interface Warp extends WireGuard {}

export interface Tailscale extends EndpointBasics, Dial {
  state_directory?: string
  auth_key?: string
  control_url?: string
  ephemeral?: boolean
  hostname?: string
  accept_routes?: boolean
  exit_node?: string
  exit_node_allow_lan_access?: boolean
  advertise_routes?: string[]
  advertise_tags?: string[]
  advertise_exit_node?: boolean
  relay_server_port?: number
  relay_server_static_endpoints?: string[]
  system_interface?: boolean
  system_interface_name?: string
  system_interface_mtu?: number
  udp_timeout?: string
}

// Create interfaces dynamically based on EpTypes keys
type InterfaceMap = {
  [Key in keyof typeof EpTypes]: {
    type: string
    [otherProperties: string]: any // You can add other properties as needed
  }
}

// Create union type from InterfaceMap
export type Endpoint = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<EpType, Endpoint> = {
  wireguard: {
    type: EpTypes.Wireguard,
    address: ['10.0.0.2/32', 'fe80::2/128'],
    private_key: '',
    listen_port: 0,
    peers: [],
    ext: { keys: [] },
  },
  warp: { type: EpTypes.Warp, address: [], private_key: '', listen_port: 0, mtu: 1420, peers: [{ address: '', port: 0, public_key: ''}] },
  tailscale: { type: EpTypes.Tailscale, domain_resolver: 'local' },
  openconnect: { type: EpTypes.OpenConnect, server: '', flavor: 'anyconnect' },
  'openvpn-client': { type: EpTypes.OpenVPNClient, mode: 'tls', network: 'udp', servers: [{ server: '', server_port: 1194 }], data_ciphers: ['AES-256-GCM'], auth: 'SHA256', tls: {} },
  'openvpn-server': { type: EpTypes.OpenVPNServer, mode: 'tls', network: 'udp', listen: '::', listen_port: 1194, address: ['10.8.0.1/24'], data_ciphers: ['AES-256-GCM'], auth: 'SHA256', tls: {} },
}

export function createEndpoint<T extends Endpoint>(type: string,json?: Partial<T>): Endpoint {
  // A stored native VPN may use legacy single-server or static-key options.
  // Creation defaults must not add a conflicting remotes list or TLS state.
  const defaults = [EpTypes.OpenVPNClient, EpTypes.OpenVPNServer].includes(type) && json && 'type' in json ? { type } : defaultValues[type]
  const endpoint: Endpoint = { ...structuredClone(defaults ?? { type }), ...(json || {}) }

  if (type === EpTypes.Wireguard) {
    endpoint.address = Array.isArray(endpoint.address) ? [...endpoint.address] : []
    endpoint.peers = Array.isArray(endpoint.peers) ? endpoint.peers.map((peer: WgPeer) => ({ ...peer })) : []
    endpoint.ext = endpoint.ext && typeof endpoint.ext === 'object'
      ? { ...endpoint.ext, keys: Array.isArray(endpoint.ext.keys) ? [...endpoint.ext.keys] : [] }
      : { keys: [] }
  }

  if (endpoint.amnezia) endpoint.amnezia = structuredClone(endpoint.amnezia)

  return endpoint
}
