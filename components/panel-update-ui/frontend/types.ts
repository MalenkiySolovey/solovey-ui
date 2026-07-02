import type { ComponentStatus as RuntimeComponentStatus } from '@/store/modules/data'

export type ComponentCatalogGroup = 'installed' | 'available' | 'unavailable'

export interface ComponentCatalogStatus extends RuntimeComponentStatus {
  latestVersion?: string
  requiredPanelVersion?: string
  availableInBinary: boolean
  compatible: boolean
  locked?: boolean
  lockedReason?: string
  installable: boolean
  removable: boolean
  group: ComponentCatalogGroup
  unavailableReason?: string
}

export interface ComponentCatalogInventory {
  binaryProfile: string
  releaseVersion?: string
  releaseSource?: string
  releaseError?: string
  components: ComponentCatalogStatus[]
  installed: ComponentCatalogStatus[]
  available: ComponentCatalogStatus[]
  unavailable: ComponentCatalogStatus[]
}
