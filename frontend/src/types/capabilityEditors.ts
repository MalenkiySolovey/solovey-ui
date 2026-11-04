import { runtimeCapability, type RuntimeCapabilities } from './runtimeCapabilities'
import type { CriticalProtocolFields } from './coreConfigContract'

export interface CapabilityEditState {
 allowed: boolean
 historical?: boolean
 message?: 'capability.waiting' | 'capability.unavailable' | 'capability.historical' | 'capability.unknown' | 'capability.contextUnsupported'
 reason?: string
 requiredProvider?: string
}
export interface CapabilityChoice { title: string; value: string }
type TransportFacts = Record<string, { available: boolean }> | undefined

// One projection for all Create/Add surfaces. Missing facts fail closed.
export function availableCreationChoices<T>(choices: readonly T[] | undefined, available: (choice: T) => boolean | undefined): T[] {
 return choices?.filter(choice => available(choice) === true) ?? []
}

// History is owned desired state, never an exception to host availability.
export function capabilityEditState(snapshot: RuntimeCapabilities | undefined, category: string, type: string, storedType = '', transport?: unknown, transports?: TransportFacts, resource?: object, criticalFields?: CriticalProtocolFields): CapabilityEditState {
 const historical = storedType !== '' && storedType === type
 if (!snapshot) return { allowed: false, historical, message: 'capability.waiting' }
 const fact = runtimeCapability(snapshot, category, type)
 const unavailable = (message: CapabilityEditState['message'], reason = fact?.reason): CapabilityEditState =>
  ({ allowed: false, historical, message: historical ? 'capability.historical' : message, reason, requiredProvider: fact?.requiredProvider })
 if (!fact?.known) return unavailable('capability.unknown')
 if (!fact.contextSupported) return unavailable('capability.contextUnsupported')
 if (!fact.available) return unavailable('capability.unavailable')
 if (resource && ['inbounds', 'outbounds'].includes(category)) {
  const side = category === 'inbounds' ? 'in' : 'out'
  const values = resource as Record<string, unknown>
  const fields = criticalFields?.[`${type}/${side}`]
  // Realm changes endpoint addressing. Missing facts cannot authorize its use.
  const critical = type === 'hysteria2' ? ['realm'] : []
  for (const field of critical) {
   if (values[field] != null && fields?.[field]?.available !== true) {
    return { ...unavailable('capability.unavailable', 'PROTOCOL_FIELD_CAPABILITY_UNAVAILABLE'), requiredProvider: fields?.[field]?.requiredProvider }
   }
  }
 }
 if (['inbounds', 'outbounds'].includes(category) && ['vmess', 'vless', 'trojan'].includes(type) && transport && typeof transport === 'object' && 'type' in transport) {
  const kind = (transport as { type?: unknown }).type
  if (typeof kind === 'string' && kind && transports?.[kind]?.available !== true) return unavailable('capability.unavailable', 'TRANSPORT_CAPABILITY_UNAVAILABLE')
 }
 return { allowed: true }
}

export function capabilityTypeChoices(snapshot: RuntimeCapabilities | undefined, category: string, catalog: Record<string, string>): CapabilityChoice[] {
 return availableCreationChoices(Object.entries(catalog).map(([title, value]) => ({ title, value })),
  choice => runtimeCapability(snapshot, category, choice.value)?.available)
}
