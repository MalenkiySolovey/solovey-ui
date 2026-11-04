import type { ConfirmOptions } from '@/components/nexus/primitives/useConfirm'
import type { SecretView } from '@/features/secretCredentialDraft'

export interface RemoteOutboundGroup {
  id: number
  subscriptionId: number
  name: string
  enabled: boolean
  outboundEnabled: boolean
  sortOrder: number
}

export interface RemoteOutboundConnection {
  id: number
  subscriptionId: number
  groupId: number
  groupIds?: number[]
  name: string
  type: string
  sourceType?: string
  convertedType?: string
  endpointId?: string
  projectionStatus?: 'EXACT' | 'SUPPORTED_WITH_APPROVED_ADAPTATION' | 'MANUAL_MAPPING_REQUIRED' | 'UNSUPPORTED'
  projectionDiagnostics?: { state: string; code: string; field: string }[]
  outboundTag: string
  enabled: boolean
  synced: boolean
  sortOrder: number
}

export interface RemoteOutboundSubscription {
  id: number
  sortOrder: number
  name: string
  url: string
  userAgent?: string
  sourceType?: 'remote' | 'inline' | 'file'
  sourcePath?: string
  content?: SecretView
  fetchHeaders?: SecretView
  include?: string
  exclude?: string
  removeEmojis?: boolean
  dialerOverride?: import('@/types/dial').Dial
  enabled: boolean
  tagPrefix: string
  autoUpdate: boolean
  updateInterval: number
  lastUpdated: number
  lastError: string
  groups?: RemoteOutboundGroup[]
  connections?: RemoteOutboundConnection[]
}

export type GroupConnectionBulkAction = 'all' | 'none' | 'invert'

export interface TestState {
  ok: boolean
  delay: number
  error: string
}

export interface CollectedProfileValue {
  value: string
  sources?: string[]
}

export interface CollectedProfileCharacteristic {
  key: string
  label: string
  values?: CollectedProfileValue[]
}

export interface CollectedProfileBlock {
  name: string
  type: string
  sources?: string[]
  characteristics?: CollectedProfileCharacteristic[]
  connections?: CollectedProfileBlock[]
}

export interface CollectedSubscriptionData {
  subscriptionId: number
  name: string
  url: string
  lastUpdated: number
  lastError?: string
  summary?: string
  profile?: CollectedProfileBlock[]
  snapshot?: unknown
  collection?: unknown
  connections: unknown[]
}

export interface SubscriptionFormRef {
  validate: () => Promise<{ valid: boolean }>
  resetValidation: () => void
}

export type ConversionFeature = 'xrayBalancer' | 'mihomoFallback' | 'mihomoLoadBalance' | 'mihomoSmart' | 'mihomoRelay' | 'mihomoSsid'

export type ConversionMode = 'original' | 'urltest' | 'selector' | 'failover' | 'balancer' | 'select' | 'url-test' | 'fallback' | 'load-balance'

export type ClientConversionTarget = 'singBox' | 'xray' | 'mihomo'

export type ConversionRules = Record<ConversionFeature, ConversionMode>

export interface RemoteConversionPolicy {
  outbound: ConversionRules
  client: {
    singBox: ConversionRules
    xray: ConversionRules
    mihomo: ConversionRules
  }
}

export type RemoteTranslate = (key: string, ...args: any[]) => string

export type RemoteConfirm = (options: ConfirmOptions) => Promise<boolean>
