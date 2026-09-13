import api from '@/plugins/api'
import type {
	FrontingApplyRequest,
	FrontingOperation,
	FrontingPlan,
	FrontingPrepareRequest,
	FrontingPreviewRequest,
	FrontingRecoveryStatus,
	FrontingRollbackRequest,
	FrontingStatusPage,
  NativeFallbackApplyRequest,
  NativeFallbackOperation,
  NativeFallbackPlan,
  NativeFallbackPrepareRequest,
  NativeFallbackPreviewRequest,
  NativeFallbackRollbackRequest,
  NativeFallbackStatusPage,
  NativeFallbackStatusQuery,
  NativeTargetInspection,
} from './types'
import type {
  LocalProxyApplyRequest,
  LocalProxyDisableRequest,
  LocalProxyPlan,
  LocalProxyPrepareRequest,
  LocalProxyResult,
  LocalProxyStatus,
} from './localProxyTypes'
import type {
  InterceptionPlan,
  InterceptionReference,
  InterceptionStatus,
} from './interceptionTypes'

export interface Envelope<T> {
  success: boolean
  msg: string
  obj: T
}

export class ProtectionAPIError extends Error {
  readonly code: string

  constructor(code: string, message: string) {
    super(message)
    this.name = 'ProtectionAPIError'
    this.code = code
  }
}

const unwrap = <T>(response: { data: Envelope<T> }): T => {
  if (!response.data?.success) {
    const errorObject = response.data?.obj && typeof response.data.obj === 'object'
      ? response.data.obj as { code?: unknown; message?: unknown }
      : undefined
    const code = typeof errorObject?.code === 'string' ? errorObject.code : 'unknown_error'
    const message = typeof errorObject?.message === 'string' ? errorObject.message : response.data?.msg || code
    throw new ProtectionAPIError(code, message)
  }
  return response.data.obj
}

// The shared client intentionally retains a legacy form-encoded POST default
// for older APIs. Server Protection is a typed JSON API, so every mutating
// request must override that default explicitly. Without this override Axios
// serializes `{ includeGeneratedNft: true }` as
// `includeGeneratedNft=true`, which the JSON-only Go handlers correctly
// reject at the first `i` byte.
const jsonRequest = {
  headers: { 'Content-Type': 'application/json' },
}

let componentLane: Promise<void> = Promise.resolve()
const activeReads = new Map<string, Promise<unknown>>()

const scheduleComponentRequest = <T>(operation: () => Promise<T>): Promise<T> => {
  const result = componentLane.then(operation, operation)
  componentLane = result.then(() => undefined, () => undefined)
  return result
}

const requestKey = (path: string, params?: Record<string, unknown>) => {
  const entries = Object.entries(params || {}).sort(([left], [right]) => left.localeCompare(right))
  return `${path}?${JSON.stringify(entries)}`
}

const getEnvelope = <T>(path: string, params?: Record<string, unknown>): Promise<{ data: Envelope<T> }> => {
  const key = requestKey(path, params)
  const current = activeReads.get(key) as Promise<{ data: Envelope<T> }> | undefined
  if (current) return current
  const pending = scheduleComponentRequest(() => api.get<Envelope<T>>(path, { params }))
  activeReads.set(key, pending)
  const clear = () => {
    if (activeReads.get(key) === pending) activeReads.delete(key)
  }
  void pending.then(clear, clear)
  return pending
}

const postJSON = <T>(path: string, body: unknown) =>
  scheduleComponentRequest(() => api.post<Envelope<T>>(path, body, jsonRequest))

const putJSON = <T>(path: string, body: unknown) =>
  scheduleComponentRequest(() => api.put<Envelope<T>>(path, body, jsonRequest))

interface PageEnvelope<T> {
  items: T[]
  page: number
  limit: number
  total: number
}

const maxCompletePages = 1024
const expectedPageLength = (total: number, limit: number, page: number) =>
  Math.max(0, Math.min(limit, total - ((page - 1) * limit)))

const allPages = async <T, P extends PageEnvelope<T>>(path: string, params: Record<string, unknown> = {}): Promise<P> => {
  const first = unwrap(await getEnvelope<P>(path, { ...params, page: 1 }))
  if (!Number.isInteger(first.page) || first.page !== 1 || !Number.isInteger(first.limit) || first.limit < 1 ||
      !Number.isInteger(first.total) || first.total < 0 || !Array.isArray(first.items)) {
    throw new ProtectionAPIError('invalid_page_contract', 'The server returned an invalid pagination contract')
  }
  if (first.items.length !== expectedPageLength(first.total, first.limit, 1)) {
    throw new ProtectionAPIError('page_contract_incomplete', 'The server catalog page length was inconsistent')
  }
  const items = [...first.items]
  const pages = Math.ceil(first.total / first.limit)
  if (pages > maxCompletePages) {
    throw new ProtectionAPIError('page_contract_too_large', 'The server catalog exceeds the bounded page walk')
  }
  for (let page = 2; page <= pages; page += 1) {
    const next = unwrap(await getEnvelope<P>(path, { ...params, page }))
    if (next.page !== page || next.limit !== first.limit || next.total !== first.total || !Array.isArray(next.items)) {
      throw new ProtectionAPIError('page_contract_changed', 'The server pagination contract changed during the page walk')
    }
    if (next.items.length !== expectedPageLength(next.total, next.limit, page)) {
      throw new ProtectionAPIError('page_contract_incomplete', 'The server catalog page length was inconsistent')
    }
    items.push(...next.items)
  }
  if (items.length !== first.total) {
    throw new ProtectionAPIError('page_contract_incomplete', 'The server catalog page walk was incomplete')
  }
  return { ...first, items } as P
}

const allNativeFallbackTargets = async (): Promise<NativeTargetInspection> => {
  const first = unwrap(await getEnvelope<NativeTargetInspection>('api/components/server-protection/target-capabilities', { page: 1 }))
  if (!Number.isInteger(first.limit) || first.limit < 1 || first.page !== 1 ||
      !Number.isInteger(first.total) || first.total < 0 || !Number.isInteger(first.totalV2) || first.totalV2 < 0 ||
      !Array.isArray(first.items) || !Array.isArray(first.targetsV2)) {
    throw new ProtectionAPIError('invalid_page_contract', 'The server returned an invalid target pagination contract')
  }
  if (first.items.length !== expectedPageLength(first.total, first.limit, 1) ||
      first.targetsV2.length !== expectedPageLength(first.totalV2, first.limit, 1)) {
    throw new ProtectionAPIError('page_contract_incomplete', 'The server target page length was inconsistent')
  }
  const items = [...first.items]
  const targetsV2 = [...first.targetsV2]
  const pages = Math.ceil(Math.max(first.total, first.totalV2) / first.limit)
  if (pages > maxCompletePages) {
    throw new ProtectionAPIError('page_contract_too_large', 'The server target catalog exceeds the bounded page walk')
  }
  for (let page = 2; page <= pages; page += 1) {
    const next = unwrap(await getEnvelope<NativeTargetInspection>('api/components/server-protection/target-capabilities', { page }))
    if (next.page !== page || next.limit !== first.limit || next.total !== first.total || next.totalV2 !== first.totalV2 ||
        !Array.isArray(next.items) || !Array.isArray(next.targetsV2)) {
      throw new ProtectionAPIError('page_contract_changed', 'The server target pagination contract changed during the page walk')
    }
    if (next.items.length !== expectedPageLength(next.total, next.limit, page) ||
        next.targetsV2.length !== expectedPageLength(next.totalV2, next.limit, page)) {
      throw new ProtectionAPIError('page_contract_incomplete', 'The server target page length was inconsistent')
    }
    items.push(...next.items)
    targetsV2.push(...next.targetsV2)
  }
  if (items.length !== first.total || targetsV2.length !== first.totalV2) {
    throw new ProtectionAPIError('page_contract_incomplete', 'The server target page walk was incomplete')
  }
  return { ...first, items, targetsV2 }
}

export const protectionAPI = {
  get: async <T>(path: string, params?: Record<string, unknown>): Promise<T> =>
    unwrap(await getEnvelope<T>(`api/components/server-protection${path}`, params)),
  getAllPages: async <T, P extends PageEnvelope<T>>(path: string, params?: Record<string, unknown>): Promise<P> =>
    allPages<T, P>(`api/components/server-protection${path}`, params),
  post: async <T>(path: string, body: unknown): Promise<T> =>
    unwrap(await postJSON<T>(`api/components/server-protection${path}`, body)),
  put: async <T>(path: string, body: unknown): Promise<T> =>
    unwrap(await putJSON<T>(`api/components/server-protection${path}`, body)),
  delete: async <T>(path: string, params?: Record<string, unknown>): Promise<T> =>
    unwrap(await scheduleComponentRequest(() => api.delete<Envelope<T>>(`api/components/server-protection${path}`, { params }))),
  nativeFallbackStatus: async (params: NativeFallbackStatusQuery = {}): Promise<NativeFallbackStatusPage> =>
    unwrap(await getEnvelope<NativeFallbackStatusPage>('api/components/server-protection/native-fallback/status', params as Record<string, unknown>)),
  nativeFallbackTargets: async (page = 1, limit?: number): Promise<NativeTargetInspection> =>
    unwrap(await getEnvelope<NativeTargetInspection>('api/components/server-protection/target-capabilities', { page, ...(limit === undefined ? {} : { limit }) })),
  nativeFallbackAllTargets: allNativeFallbackTargets,
  nativeFallbackPreview: async (body: NativeFallbackPreviewRequest): Promise<NativeFallbackPlan> =>
    unwrap(await postJSON<NativeFallbackPlan>('api/components/server-protection/native-fallback/preview', body)),
  nativeFallbackPrepare: async (body: NativeFallbackPrepareRequest): Promise<NativeFallbackOperation> =>
    unwrap(await postJSON<NativeFallbackOperation>('api/components/server-protection/native-fallback/prepare', body)),
  nativeFallbackApply: async (body: NativeFallbackApplyRequest): Promise<NativeFallbackOperation> =>
    unwrap(await postJSON<NativeFallbackOperation>('api/components/server-protection/native-fallback/apply', body)),
  nativeFallbackRollback: async (body: NativeFallbackRollbackRequest): Promise<NativeFallbackOperation> =>
    unwrap(await postJSON<NativeFallbackOperation>('api/components/server-protection/native-fallback/rollback', body)),
	frontingStatus: async (): Promise<FrontingStatusPage> =>
		unwrap(await getEnvelope<FrontingStatusPage>('api/components/server-protection/fronting/status')),
	frontingPreview: async (body: FrontingPreviewRequest): Promise<FrontingPlan> =>
		unwrap(await postJSON<FrontingPlan>('api/components/server-protection/fronting/preview', body)),
	frontingPrepare: async (body: FrontingPrepareRequest): Promise<FrontingOperation> =>
		unwrap(await postJSON<FrontingOperation>('api/components/server-protection/fronting/prepare', body)),
	frontingApply: async (body: FrontingApplyRequest): Promise<FrontingOperation> =>
		unwrap(await postJSON<FrontingOperation>('api/components/server-protection/fronting/apply', body)),
	frontingRollback: async (body: FrontingRollbackRequest): Promise<FrontingOperation> =>
		unwrap(await postJSON<FrontingOperation>('api/components/server-protection/fronting/rollback', body)),
	frontingOperation: async (operationId: string): Promise<FrontingOperation> =>
		unwrap(await getEnvelope<FrontingOperation>(`api/components/server-protection/fronting/operations/${encodeURIComponent(operationId)}`)),
	frontingRecovery: async (operationId: string): Promise<FrontingRecoveryStatus> =>
		unwrap(await getEnvelope<FrontingRecoveryStatus>(`api/components/server-protection/fronting/operations/${encodeURIComponent(operationId)}/recovery`)),
  localProxyStatus: async (refresh = false): Promise<LocalProxyStatus> =>
    unwrap(await getEnvelope<LocalProxyStatus>('api/components/server-protection/local-proxy/status', { refresh })),
  localProxyPreview: async (body: { resourceId: string; endpointId: string; factRevision: string }): Promise<LocalProxyPlan> =>
    unwrap(await postJSON<LocalProxyPlan>('api/components/server-protection/local-proxy/preview', body)),
  localProxyPrepare: async (body: LocalProxyPrepareRequest): Promise<LocalProxyResult> =>
    unwrap(await postJSON<LocalProxyResult>('api/components/server-protection/local-proxy/prepare', body)),
  localProxyApply: async (body: LocalProxyApplyRequest): Promise<LocalProxyResult> =>
    unwrap(await postJSON<LocalProxyResult>('api/components/server-protection/local-proxy/apply', body)),
  localProxyDisable: async (body: LocalProxyDisableRequest): Promise<LocalProxyResult> =>
    unwrap(await postJSON<LocalProxyResult>('api/components/server-protection/local-proxy/disable', body)),
  interceptionStatus: async (): Promise<InterceptionStatus> =>
    unwrap(await getEnvelope<InterceptionStatus>('api/components/server-protection/interception/status')),
  interceptionPreview: async (interception: InterceptionReference): Promise<InterceptionPlan> =>
    unwrap(await postJSON<InterceptionPlan>('api/components/server-protection/interception/preview', { interception })),
}
