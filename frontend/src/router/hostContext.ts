// Host identity comes from each tab's URL. There is no persisted selected Host.
export const LOCAL_HOST = Object.freeze({ kind: 'LOCAL', id: 'local' } as const)

export const routeHostID = (route: { params: Record<string, unknown> }): string | undefined =>
  typeof route.params.hostId === 'string' ? route.params.hostId : undefined

export const safeHostReturnTo = (value: unknown): string | undefined =>
  typeof value === 'string' && /^\/hosts\/local(?:[/?#]|$)/.test(value) && !value.includes('\\') ? value : undefined

export const hostPath = (path: string, hostID = LOCAL_HOST.id as string): string => {
  if (path.startsWith('/hosts/')) return path
  return `/hosts/${encodeURIComponent(hostID)}${path === '/' ? '' : path}`
}

export const hostIDFromPath = (pathname: string, base: string): string => {
  const path = pathname.startsWith(base) ? pathname.slice(base.length) : pathname.replace(/^\//, '')
  const match = /^hosts\/([^/]+)(?:\/|$)/.exec(path)
  if (!match) return path.startsWith('hosts/') ? 'invalid' : LOCAL_HOST.id
  try { return decodeURIComponent(match[1]) } catch { return 'invalid' }
}

export const currentHostID = (base: string): string =>
  hostIDFromPath(typeof window === 'undefined' ? '' : window.location.pathname, base)

export const hostChangeAllowed = (
  to: { params: Record<string, unknown> },
  from: { params: Record<string, unknown> },
  dirty: boolean,
  discard: () => boolean,
): boolean => !dirty || routeHostID(to) === routeHostID(from) || discard()
