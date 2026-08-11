import HttpUtils, { type Msg } from '@/plugins/httputil'
import { clearCSRFToken } from '@/store/csrf'

export type AuthState = 'authenticated' | 'password_reset' | 'mfa_pending' | 'mfa_recovery'
export type Assurance = 'password' | 'mfa' | 'recovery'

export interface MFAStatus {
  enabled: boolean
  pending: boolean
  pendingExpiresAt: number
  recoveryAcknowledged: boolean
  recoveryRemaining: number
  awaitingAcknowledgment: boolean
}

export interface SecurityPosture {
  authState: AuthState
  assurance: Assurance
  username: string
  sessionRef: string
  lastMfaAt: number
  sessionLifetimePolicy: 'bounded_v1' | 'legacy_unbounded' | 'legacy_explicit'
  passwordPolicyVersion: number
  passwordPolicyCurrentVersion: number
  forcePasswordReset: boolean
  mfa: MFAStatus
  clientIdentity: {
    version: number
    provenance: string
    clientPrefix: string
    trustedProxyHops: number
    trustedProxyCount: number
    trustedProxySource: string
    trustedProxyCidrs: string[]
    warningCodes: string[]
    actualScheme: string
    desiredScheme: string
    schemeSource: string
    forwardedValid: boolean
    configRevision: string
  }
  cookiePolicy: {
    httpOnly: boolean
    path: string
    sameSite: string
    secure: boolean
  }
  stepUpTargets: {
    revokeOthers: string
    adoptBounded: string
    self: string
  }
}

export interface SecuritySession {
  ref: string
  current: boolean
  authState: AuthState
  assurance: Assurance
  lastMfaAt: number
  lifetimePosture: string
  createdAt: number
  authenticatedAt: number
  lastSeenAt: number
  idleExpiresAt: number
  absoluteExpiresAt: number
  rememberedExpiresAt: number
  clientProvenance: string
  clientPrefix: string
  deviceLabel: string
  revokedAt: number
  revokedReason?: string
}

export interface SessionInventory {
  items: SecuritySession[]
  nextCursor?: string
}

const jsonOptions = {
  headers: { 'Content-Type': 'application/json' },
}

export const getSecurityPosture = () => HttpUtils.get('api/v1/security/posture')
export const getSecuritySessions = () => HttpUtils.get('api/v1/security/sessions', { limit: 50 })
export const transitionPassword = (payload: {
  currentPassword: string
  newUsername: string
  newPassword: string
}) => HttpUtils.post('api/v1/security/password/transition', payload, jsonOptions)
export const changePassword = (payload: {
  newUsername: string
  newPassword: string
  stepUpToken: string
}) => HttpUtils.post('api/v1/security/password/change', payload, jsonOptions)
export const completeMFAChallenge = (code: string) =>
  HttpUtils.post('api/v1/security/mfa/challenge', { code }, jsonOptions)
export const completeRecoveryChallenge = (code: string) =>
  HttpUtils.post('api/v1/security/mfa/recovery', { code }, jsonOptions)
export const completeMFARecovery = (payload: { newUsername: string; newPassword: string }) =>
  HttpUtils.post('api/v1/security/mfa/recovery/complete', payload, jsonOptions)
export const issueStepUp = async (payload: {
  method: 'password' | 'totp' | 'recovery'
  credential: string
  operationKind: string
  targetDigest: string
}) => {
  const response = await HttpUtils.post('api/v1/security/step-up', payload, jsonOptions)
  // A successful privilege verification rotates the server-side CSRF value.
  // Clear the in-memory copy before the protected action is submitted.
  clearCSRFToken()
  return response
}
export const beginMFAEnrollment = (stepUpToken: string) =>
  HttpUtils.post('api/v1/security/mfa/enroll', { stepUpToken }, jsonOptions)
export const confirmMFAEnrollment = (code: string) =>
  HttpUtils.post('api/v1/security/mfa/confirm', { code }, jsonOptions)
export const acknowledgeRecoveryCodes = () =>
  HttpUtils.post('api/v1/security/mfa/recovery/ack', { acknowledged: true }, jsonOptions)
export const rotateRecoveryCodes = (stepUpToken: string) =>
  HttpUtils.post('api/v1/security/mfa/recovery/rotate', { stepUpToken }, jsonOptions)
export const disableMFA = (stepUpToken: string) =>
  HttpUtils.post('api/v1/security/mfa/disable', { stepUpToken }, jsonOptions)
export const revokeSession = (ref: string) =>
  HttpUtils.post('api/v1/security/sessions/revoke', { ref }, jsonOptions)
export const revokeOtherSessions = (stepUpToken: string) =>
  HttpUtils.post('api/v1/security/sessions/revoke-others', { stepUpToken }, jsonOptions)
export const adoptBoundedSessions = (stepUpToken: string) =>
  HttpUtils.post('api/v1/security/sessions/adopt-bounded', { stepUpToken }, jsonOptions)

export const securityTargetDigest = async (value: string): Promise<string> => {
  const bytes = new TextEncoder().encode(value)
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
}

export const acquireStepUpToken = async (
  operationKind: string,
  target: string,
  credential: string,
): Promise<{ token: string | null; response: Msg }> => {
  const postureResponse = await getSecurityPosture()
  const posture = messageObject<SecurityPosture>(postureResponse)
  if (!posture) return { token: null, response: postureResponse }
  const targetDigest = target === '$self'
    ? posture.stepUpTargets.self
    : await securityTargetDigest(target)
  const normalized = credential.trim()
  const method: 'password' | 'totp' | 'recovery' = posture.mfa.enabled
    ? (/^\d{6}$/.test(normalized) ? 'totp' : 'recovery')
    : 'password'
  const response = await issueStepUp({
    method,
    credential: method === 'password' ? credential : normalized,
    operationKind,
    targetDigest,
  })
  return {
    token: messageObject<{ token: string }>(response)?.token ?? null,
    response,
  }
}

export const messageObject = <T>(message: Msg): T | null => {
  if (!message.success || !message.obj) return null
  return message.obj as T
}
