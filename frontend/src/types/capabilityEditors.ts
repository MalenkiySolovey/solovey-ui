import { runtimeCapability, type RuntimeCapabilities } from './runtimeCapabilities'

export interface CapabilityEditState {
 allowed: boolean
 message?: 'capability.waiting' | 'capability.unavailable' | 'capability.historical' | 'capability.unknown' | 'capability.contextUnsupported'
 reason?: string
}
export interface CapabilityChoice { title: string; value: string; props: { disabled: boolean } }

// Thin projection of backend facts. The submitted historical identity is only
// a UI hint; the backend separately verifies the real stored row and ID.
export function capabilityEditState(snapshot: RuntimeCapabilities | undefined, category: string, type: string, storedType = ''): CapabilityEditState {
 if (!snapshot) return { allowed: false, message: 'capability.waiting' }
 const fact = runtimeCapability(snapshot, category, type)
 if (!fact?.known) return { allowed: false, message: 'capability.unknown', reason: fact?.reason }
 if (!fact.contextSupported) return { allowed: false, message: 'capability.contextUnsupported', reason: fact.reason }
 if (fact?.available) return { allowed: true }
 if (fact?.known && fact.contextSupported && storedType === type) return { allowed: true, message: 'capability.historical', reason: fact.reason }
 return { allowed: false, message: 'capability.unavailable', reason: fact?.reason }
}

export function capabilityTypeChoices(snapshot: RuntimeCapabilities | undefined, category: string, catalog: Record<string, string>, storedType = ''): CapabilityChoice[] {
 const choices = Object.entries(catalog).map(([title, value]) => ({ title, value, props: { disabled: !capabilityEditState(snapshot, category, value, storedType).allowed } }))
 if (storedType && !choices.some(choice => choice.value === storedType)) choices.push({ title: storedType, value: storedType, props: { disabled: !capabilityEditState(snapshot, category, storedType, storedType).allowed } })
 return choices
}
