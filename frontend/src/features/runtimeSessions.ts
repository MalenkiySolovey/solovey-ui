export interface RuntimeStatus {
  generation: string
  state: string
  apiAvailable: boolean
  observedAt: number
  reason?: string
}
export interface RuntimeFlow {
  id: string
  kind: 'flow'
  status: 'active'
  clientId?: number
  identity: 'verified' | 'unavailable'
  inbound: string
  inboundType: string
  outbound: string
  network: string
  protocol: string
  source: string
  destination: string
  createdAt: number
  upload: number
  download: number
  parentControl: 'not_supported'
}
export interface SessionView {
  status: RuntimeStatus
  maintenance: boolean
	maintenanceAvailable?: boolean
  reason?: string
  capabilities: { compiled: boolean; flowClose: boolean; parentClose: boolean }
  snapshot?: {
    generation: string
    observedAt: number
    connections: RuntimeFlow[]
    total: number
    actualTotal: number
    unassociated: number
    limit: number
    truncated: boolean
  }
}
export interface DisconnectResult {
  generation: string
  outcome: string
  matched: number
  closed: number
  remaining: number
  parentClosed: boolean
  parentControl: 'not_supported'
  reason?: string
}

export function runtimeAvailability(view: SessionView): string | null {
  if (view.maintenanceAvailable === false) return 'unavailable'
  if (view.maintenance) return 'maintenance'
  if (view.reason === 'stale_generation') return 'stale'
  if (view.reason === 'runtime_limit_exceeded') return 'limited'
  if (view.reason) return 'unavailable'
  if (view.status.state === 'stopped_by_error') return 'failed'
  if (view.status.state !== 'running') return ['stopped', 'starting', 'stopping', 'failed'].includes(view.status.state) ? view.status.state : 'unavailable'
  if (!view.capabilities.compiled || !view.status.apiAvailable) return 'unavailable'
  return null
}

export function sessionState(view: SessionView, now: number): string {
  const availability = runtimeAvailability(view)
  if (availability) return availability
  if (!view.snapshot) return 'unavailable'
  if (view.snapshot.generation !== view.status.generation || now - view.snapshot.observedAt > 10000) return 'stale'
  if (view.snapshot.connections.length) return 'active'
  return view.snapshot.unassociated ? 'identity_unavailable' : 'empty'
}
