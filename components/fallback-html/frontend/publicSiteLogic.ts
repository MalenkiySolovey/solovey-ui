export interface RuntimeEndpoint {
  runtime?: string
  listen?: string
  port?: number
  status?: string
  reason?: string
}

const reservedPublicPrefixes = [
  '/api/',
  '/apiv2/',
  '/assets/',
  '/sub/',
  '/.well-known/acme-challenge/',
]

export function endpointChipColor(status?: string): string {
  switch (status) {
    case 'available':
    case 'active':
      return 'success'
    case 'stale':
    case 'planned':
    case 'validating':
    case 'applying':
    case 'rollback':
      return 'warning'
    case 'unsupported':
    case 'blocked':
    case 'blocked-external':
    case 'blocked-inbound':
    case 'loopback-fallback':
    case 'failed':
      return 'error'
    case 'not-targeted':
    case 'removed':
    case 'free':
    case 'managed':
      return 'info'
    default:
      return 'grey'
  }
}

export function endpointLabel(endpoint: RuntimeEndpoint): string {
  const runtime = endpoint.runtime?.trim() || 'runtime'
  const listen = endpoint.listen?.trim() || '0.0.0.0'
  return `${runtime} / ${listen}:${endpoint.port ?? 0}`
}

export function endpointStatusText(endpoint: RuntimeEndpoint): string {
  const status = endpoint.status?.trim() || 'unknown'
  const reason = endpoint.reason?.trim()
  return reason ? `${status}: ${reason}` : status
}

export function endpointChipTitle(endpoint: RuntimeEndpoint): string {
  return `${endpointLabel(endpoint)} - ${endpointStatusText(endpoint)}`
}

export function normalizePublicPathPreview(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return '/'
  if (trimmed.includes('\\')) return trimmed
  let path = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  path = path.replace(/\/{2,}/g, '/')
  if (path !== '/' && !path.endsWith('/') && !path.split('/').pop()?.includes('.')) {
    path += '/'
  }
  return path
}

export function isExternalURL(value: string): boolean {
  return /^https?:\/\//i.test(value.trim())
}

export function isReservedPublicPath(value: string, extraPrefixes: string[] = []): boolean {
  if (isExternalURL(value)) return false
  const path = normalizePublicPathPreview(value)
  if (path === '/') return false
  return [...reservedPublicPrefixes, ...normalizeReservedPrefixes(extraPrefixes)]
    .some(prefix => path === prefix.slice(0, -1) || path.startsWith(prefix))
}

function normalizeReservedPrefixes(prefixes: string[]): string[] {
  return prefixes
    .map(prefix => normalizePublicPathPreview(prefix))
    .filter(prefix => prefix !== '/')
}
