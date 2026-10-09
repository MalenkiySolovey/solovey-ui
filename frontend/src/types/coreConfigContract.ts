import { shallowRef } from 'vue'
import HttpUtils from '@/plugins/httputil'

export interface SnellCapability {
  versions: { version: number; clientVersion: number; pskMinBytes: number; pskMaxBytes: number; obfuscation: boolean }[]
  obfsModes: string[]
  modes: string[]
  userKeyMaxBytes: number
  uri: boolean
}

export interface CoreConfigContract {
	compatibilityCatalogue?: { id: string; consumer: string; classification: string; policy: string }[]
  tls?: {
    fields: Record<string, string[]>
    providerFields: Record<string, string[]>
    providerTypes: string[]
    providerUnavailable: Record<string, string>
    providerModes: string[]
    clientAuthentication: string[]
    engines: string[]
  }
  protocol?: { fields: Record<string, string[]>; memoryUnits: Record<string, number>; snell?: Record<'in' | 'out', SnellCapability> }
  dnsActions: Record<string, string[]>
  dnsConditions: string[]
  dnsCacheFields?: string[]
  tunDnsModes: string[]
  tunDnsUnavailableModes?: Record<string, string>
  maxRuleDepth: number
  maxRuleNodes: number
	 httpClientFields?: Record<string, string[]>
	 httpEngines?: string[]
	 httpVersions?: string[]
	 httpUnavailableEngines?: Record<string, string>
	 httpUnavailableVersions?: Record<string, string>
	 directHttpClient?: Record<string, unknown>
}
export interface CompatibilityFinding {
  kind: string
  path: string
  code: string
  severity: 'warn' | 'error'
  message: string
  migrationOutcome?: string
  automaticAvailable?: boolean
  operatorActionRequired?: boolean
}
export interface CompatibilityPreview {
  outcome: string
  findings: CompatibilityFinding[] | null
  blocked: boolean
  dns?: Record<string, unknown>
	 http_clients?: Record<string, unknown>[]
	 route?: Record<string, unknown>
	 subscriptionTemplate?: Record<string, unknown>
}

// Both layouts and modals share facts from the backend owner. A failed fetch
// leaves the existing draft intact and does not invent a replacement schema.
export const coreConfigContract = shallowRef<CoreConfigContract>()
let loadingContract: Promise<void> | undefined
export function loadCoreConfigContract(): Promise<void> {
  if (coreConfigContract.value) return Promise.resolve()
  if (loadingContract) return loadingContract
  loadingContract = (async () => {
    const response = await HttpUtils.get('api/editor-contract')
    if (response.success && response.obj?.dnsActions) coreConfigContract.value = response.obj
  })().finally(() => { loadingContract = undefined })
  return loadingContract
}

// The panel's general POST default is form encoding; this owner endpoint takes
// a JSON object so nested drafts and explicit false/zero values remain intact.
export function previewCoreCompatibility(request: { config?: object; subscriptionTemplate?: object; includeHttp?: boolean; prepareHttpDownloads?: boolean }) {
  return HttpUtils.post('api/compatibility-preview', request, { headers: { 'Content-Type': 'application/json' } })
}

export function serializeDNSRule(draft: Record<string, any>, originalAction: string | undefined, contract: CoreConfigContract | undefined): Record<string, any> {
  const result = JSON.parse(JSON.stringify(draft))
  if (contract && originalAction !== undefined && originalAction !== (result.action ?? 'route')) {
    const owned = new Set(Object.values(contract.dnsActions).flat())
    const allowed = new Set(contract.dnsActions[result.action ?? 'route'] ?? [])
    for (const key of owned) if (!allowed.has(key)) delete result[key]
  }
  if (result.type === 'simple') {
    const conditions = result.rules?.[0] ?? {}
    delete result.rules
    delete result.mode
    delete result.type
    return { ...conditions, ...result }
  }
  return result
}
