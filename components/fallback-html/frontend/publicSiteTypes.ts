export interface Page {
  id: number
  path: string
  title: string
  body: string
  contentMode: string
  isHome: boolean
}

export interface Redirect {
  id: number
  fromPath: string
  toPath: string
  statusCode: number
  external: boolean
}

export interface AssetView {
  id: number
  logicalPath: string
  mimeType: string
  sha256: string
  sizeBytes: number
  provenance: string
  createdAt: number
}

export interface ExternalResourceView {
  id: number
  kind: string
  url: string
  allowed: boolean
  createdAt: number
}

export interface Site {
  id: number
  name: string
  enabled: boolean
  status: string
  templateId: string
  hostname?: string
  pages: Page[]
  redirects: Redirect[]
}

export interface TargetView {
  id: number
  kind: string
  host: string
  listen: string
  port: number
  rootPath: string
  runtime: string
  tls: boolean
  status: string
  reason: string
  current: boolean
}

export interface PortCandidate {
  kind: string
  listen: string
  port: number
  runtime: string
  tls: boolean
  status: string
  reason: string
}

export interface SafetyReport {
  ok: boolean
  warnings: string[]
}

export interface PreviewResult {
  path: string
  html: string
  warnings: string[]
}

export interface TemplateDefinition {
  id: string
  name: string
  source: string
  license: string
  contentTypeProfile: string
  renderable: boolean
}

export interface RemoteTemplateView {
  id: string
  name: string
  source: string
  license: string
  contentTypeProfile: string
  manifestUrl: string
  installed: boolean
  installedAt: number
  notes: string[]
}

export interface PublishView {
  id: number
  version: string
  active: boolean
  files: number
  createdAt: number
}

export interface PrunePublishesResult {
  removed: number
  kept: number
}

export interface ProviderStatusView {
  targetId: string
  endpointMode: string
  readiness: string
  healthFreshness: string
  healthObservedAt: number
  healthExpiresAt: number
  capacityState: string
  capacitySlotsUsed: number
  capacitySlotsTotal: number
  inUse: boolean
  reconcileRequired: boolean
  reservations: { state: string, count: number }[]
  reasonCodes: string[]
}
