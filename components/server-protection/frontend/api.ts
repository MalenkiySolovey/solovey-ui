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

export const protectionAPI = {
  get: async <T>(path: string, params?: Record<string, unknown>): Promise<T> =>
    unwrap(await api.get<Envelope<T>>(`api/components/server-protection${path}`, { params })),
  post: async <T>(path: string, body: unknown): Promise<T> =>
    unwrap(await api.post<Envelope<T>>(`api/components/server-protection${path}`, body)),
  put: async <T>(path: string, body: unknown): Promise<T> =>
    unwrap(await api.put<Envelope<T>>(`api/components/server-protection${path}`, body)),
  delete: async <T>(path: string, params?: Record<string, unknown>): Promise<T> =>
    unwrap(await api.delete<Envelope<T>>(`api/components/server-protection${path}`, { params })),
  nativeFallbackStatus: async (params: NativeFallbackStatusQuery = {}): Promise<NativeFallbackStatusPage> =>
    unwrap(await api.get<Envelope<NativeFallbackStatusPage>>('api/components/server-protection/native-fallback/status', { params })),
  nativeFallbackTargets: async (page = 1, limit = 200): Promise<NativeTargetInspection> =>
    unwrap(await api.get<Envelope<NativeTargetInspection>>('api/components/server-protection/target-capabilities', { params: { page, limit } })),
  nativeFallbackPreview: async (body: NativeFallbackPreviewRequest): Promise<NativeFallbackPlan> =>
    unwrap(await api.post<Envelope<NativeFallbackPlan>>('api/components/server-protection/native-fallback/preview', body)),
  nativeFallbackPrepare: async (body: NativeFallbackPrepareRequest): Promise<NativeFallbackOperation> =>
    unwrap(await api.post<Envelope<NativeFallbackOperation>>('api/components/server-protection/native-fallback/prepare', body)),
  nativeFallbackApply: async (body: NativeFallbackApplyRequest): Promise<NativeFallbackOperation> =>
    unwrap(await api.post<Envelope<NativeFallbackOperation>>('api/components/server-protection/native-fallback/apply', body)),
  nativeFallbackRollback: async (body: NativeFallbackRollbackRequest): Promise<NativeFallbackOperation> =>
    unwrap(await api.post<Envelope<NativeFallbackOperation>>('api/components/server-protection/native-fallback/rollback', body)),
	frontingStatus: async (): Promise<FrontingStatusPage> =>
		unwrap(await api.get<Envelope<FrontingStatusPage>>('api/components/server-protection/fronting/status')),
	frontingPreview: async (body: FrontingPreviewRequest): Promise<FrontingPlan> =>
		unwrap(await api.post<Envelope<FrontingPlan>>('api/components/server-protection/fronting/preview', body)),
	frontingPrepare: async (body: FrontingPrepareRequest): Promise<FrontingOperation> =>
		unwrap(await api.post<Envelope<FrontingOperation>>('api/components/server-protection/fronting/prepare', body)),
	frontingApply: async (body: FrontingApplyRequest): Promise<FrontingOperation> =>
		unwrap(await api.post<Envelope<FrontingOperation>>('api/components/server-protection/fronting/apply', body)),
	frontingRollback: async (body: FrontingRollbackRequest): Promise<FrontingOperation> =>
		unwrap(await api.post<Envelope<FrontingOperation>>('api/components/server-protection/fronting/rollback', body)),
	frontingOperation: async (operationId: string): Promise<FrontingOperation> =>
		unwrap(await api.get<Envelope<FrontingOperation>>(`api/components/server-protection/fronting/operations/${encodeURIComponent(operationId)}`)),
	frontingRecovery: async (operationId: string): Promise<FrontingRecoveryStatus> =>
		unwrap(await api.get<Envelope<FrontingRecoveryStatus>>(`api/components/server-protection/fronting/operations/${encodeURIComponent(operationId)}/recovery`)),
  localProxyStatus: async (refresh = false): Promise<LocalProxyStatus> =>
    unwrap(await api.get<Envelope<LocalProxyStatus>>('api/components/server-protection/local-proxy/status', { params: { refresh } })),
  localProxyPreview: async (body: { resourceId: string; endpointId: string; factRevision: string }): Promise<LocalProxyPlan> =>
    unwrap(await api.post<Envelope<LocalProxyPlan>>('api/components/server-protection/local-proxy/preview', body)),
  localProxyPrepare: async (body: LocalProxyPrepareRequest): Promise<LocalProxyResult> =>
    unwrap(await api.post<Envelope<LocalProxyResult>>('api/components/server-protection/local-proxy/prepare', body)),
  localProxyApply: async (body: LocalProxyApplyRequest): Promise<LocalProxyResult> =>
    unwrap(await api.post<Envelope<LocalProxyResult>>('api/components/server-protection/local-proxy/apply', body)),
  localProxyDisable: async (body: LocalProxyDisableRequest): Promise<LocalProxyResult> =>
    unwrap(await api.post<Envelope<LocalProxyResult>>('api/components/server-protection/local-proxy/disable', body)),
  interceptionStatus: async (): Promise<InterceptionStatus> =>
    unwrap(await api.get<Envelope<InterceptionStatus>>('api/components/server-protection/interception/status')),
  interceptionPreview: async (interception: InterceptionReference): Promise<InterceptionPlan> =>
    unwrap(await api.post<Envelope<InterceptionPlan>>('api/components/server-protection/interception/preview', { interception })),
}
