import { shallowRef } from 'vue'
import HttpUtils from '@/plugins/httputil'

export interface CoreConfigContract {
  dnsActions: Record<string, string[]>
  dnsConditions: string[]
  tunDnsModes: string[]
  maxRuleDepth: number
  maxRuleNodes: number
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
}

// Both layouts and modals share facts from the backend owner. A failed fetch
// leaves the existing draft intact and does not invent a replacement schema.
export const coreConfigContract = shallowRef<CoreConfigContract>()
let loadingContract: Promise<void> | undefined
export function loadCoreConfigContract(): Promise<void> {
  if (coreConfigContract.value) return Promise.resolve()
  if (loadingContract) return loadingContract
  loadingContract = (async () => {
    const response = await HttpUtils.get('api/config/editor-contract')
    if (response.success && response.obj?.dnsActions) coreConfigContract.value = response.obj
  })().finally(() => { loadingContract = undefined })
  return loadingContract
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
