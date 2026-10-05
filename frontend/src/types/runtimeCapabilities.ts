export interface RuntimeCapability {
  category: string
  type: string
  runtimeType: string
  known: boolean
  contextSupported: boolean
  registered: boolean
  compiled: boolean
  available: boolean
  buildTag?: string
  platform?: string
  reason?: string
}

export interface RuntimeCapabilities {
  schema: 'solovey-ui/entity-capabilities/v1'
  componentProfile: 'full' | 'minimal'
  facts: RuntimeCapability[]
}

// This is only wire validation. Support and aliases come from the backend.
export function readRuntimeCapabilities(value: unknown): RuntimeCapabilities | undefined {
  if (!value || typeof value !== 'object') return undefined
  const data = value as Record<string, unknown>
  if (data.schema !== 'solovey-ui/entity-capabilities/v1' ||
      !['full', 'minimal'].includes(String(data.componentProfile)) || !Array.isArray(data.facts)) return undefined
  const seen = new Set<string>()
  for (const item of data.facts) {
    if (!item || typeof item !== 'object') return undefined
    const fact = item as Record<string, unknown>
    if (['category', 'type', 'runtimeType'].some(key => typeof fact[key] !== 'string' || !fact[key]) ||
        ['known', 'contextSupported', 'registered', 'compiled', 'available'].some(key => typeof fact[key] !== 'boolean') ||
        fact.available !== (fact.known && fact.contextSupported && fact.registered && fact.compiled) ||
        (fact.buildTag !== undefined && typeof fact.buildTag !== 'string') ||
        (fact.platform !== undefined && typeof fact.platform !== 'string') ||
        (fact.reason !== undefined && typeof fact.reason !== 'string')) return undefined
    const key = `${fact.category}:${fact.type}`
    if (seen.has(key)) return undefined
    seen.add(key)
  }
  return structuredClone(value) as RuntimeCapabilities
}

export function runtimeCapability(value: RuntimeCapabilities | undefined, category: string, type: string): RuntimeCapability | undefined {
  return value?.facts.find(fact => fact.category === category && fact.type === type)
}
