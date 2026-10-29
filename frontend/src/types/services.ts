import { Listen } from "./inbounds"
import { iTls } from "./tls"
import { secretMutation, type SecretView, type SecretMutation } from '@/features/secretCredentialDraft'

export const SrvTypes = {
  DERP: 'derp',
  Resolved: 'resolved',
  SSMAPI: 'ssm-api',
  OOMKiller: 'oom-killer',
  HysteriaRealm: 'hysteria-realm',
}

type SrvType = typeof SrvTypes[keyof typeof SrvTypes]

interface SrvBasics extends Listen {
  id: number
  type: SrvType
  tag: string
  tls_id: number
}

export interface DERP extends SrvBasics {
  tls: iTls
  config_path: string
  verify_client_endpoint?: string[]
  verify_client_url?: any[]
  home?: string
  mesh_with?: any[]
  mesh_psk?: string
  mesh_psk_file?: string
  stun?: any
}

export interface Resolved extends SrvBasics {}

export interface SSMAPI extends SrvBasics {
  servers: any
  cache_path?: string
  tls?: iTls
}

export interface OOMKiller extends SrvBasics {
  memory_limit?: string | number
  safety_margin?: string | number
  min_interval?: string
  max_interval?: string
  checks_before_limit?: number
}

export interface RealmIdentity {
  name: string
  token?: SecretView | SecretMutation
  max_realms?: number
}

export interface HysteriaRealm extends SrvBasics {
  users: RealmIdentity[]
  tls?: iTls
  idle_timeout?: string
  keep_alive_period?: string
  stream_receive_window?: string | number
  connection_receive_window?: string | number
  max_concurrent_streams?: number
}

export function realmServiceInvalid(value: Srv): boolean {
  if (value.type !== SrvTypes.HysteriaRealm) return false
  const users = (value as HysteriaRealm).users
  const names = new Set<string>()
  if (!Array.isArray(users) || !users.length) return true
  for (const user of users) {
    if (!user.name?.trim() || names.has(user.name) || (user.max_realms !== undefined && (!Number.isInteger(user.max_realms) || user.max_realms < 0))) return true
    names.add(user.name)
    try {
      const token = secretMutation(user.token)
      if (!token || token.intent === 'clear' || token.intent === 'unchanged' && !token.present) return true
    } catch { return true }
  }
  return false
}

export function prepareServiceSave(value: Srv): Srv {
  if (value.type !== SrvTypes.HysteriaRealm) return value
  const realm = value as HysteriaRealm
  return { ...realm, users: realm.users.map(user => ({ ...user, token: secretMutation(user.token) })) }
}

type InterfaceMap = {
  derp: DERP
  resolved: Resolved
  'ssm-api': SSMAPI
  'oom-killer': OOMKiller
  'hysteria-realm': HysteriaRealm
}

export type Srv = InterfaceMap[keyof InterfaceMap]

const defaultValues: Record<SrvType, Srv> = {
  derp: <DERP>{ type: 'derp', config_path: '', tls_id:0 },
  resolved: <Resolved>{ type: 'resolved', listen: '::', listen_port: 53 },
  'ssm-api': <SSMAPI>{ type: 'ssm-api', tls_id: 0, servers: {} },
  'oom-killer': { type: 'oom-killer', id: 0, tag: '', checks_before_limit: 3 } as OOMKiller,
  'hysteria-realm': { type: 'hysteria-realm', id: 0, tag: '', listen: '::', listen_port: 0, tls_id: 0, users: [] } as HysteriaRealm,
}

export function createSrv<T extends Srv>(type: string, json?: Partial<T>): Srv {
  const defaultObject: Srv = { ...defaultValues[type], ...(json || {}) }
  if (type === SrvTypes.HysteriaRealm && (!json || !('users' in json))) (defaultObject as HysteriaRealm).users = []
  return defaultObject
}
