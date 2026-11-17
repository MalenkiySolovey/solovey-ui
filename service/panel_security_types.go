package service

import (
	panelsecurity "github.com/MalenkiySolovey/solovey-backend/security"
)

// Compatibility names forward to the canonical Panel security owner.
const (
	AuthStateAuthenticated          = panelsecurity.AuthStateAuthenticated
	AuthStatePasswordReset          = panelsecurity.AuthStatePasswordReset
	AuthStateMFAPending             = panelsecurity.AuthStateMFAPending
	AuthStateMFARecovery            = panelsecurity.AuthStateMFARecovery
	SessionStateActive              = panelsecurity.SessionStateActive
	SessionStatePreAuth             = panelsecurity.SessionStatePreAuth
	SessionStateRevoked             = panelsecurity.SessionStateRevoked
	AssurancePassword               = panelsecurity.AssurancePassword
	AssuranceMFA                    = panelsecurity.AssuranceMFA
	AssuranceRecovery               = panelsecurity.AssuranceRecovery
	LifetimePostureBoundedV1        = panelsecurity.LifetimePostureBoundedV1
	LifetimePostureLegacyExplicit   = panelsecurity.LifetimePostureLegacyExplicit
	LifetimePostureLegacyUnbounded  = panelsecurity.LifetimePostureLegacyUnbounded
	LifetimePostureLegacy           = panelsecurity.LifetimePostureLegacy
	DefaultSessionIdle              = panelsecurity.DefaultSessionIdle
	DefaultSessionAbsolute          = panelsecurity.DefaultSessionAbsolute
	DefaultRememberedSession        = panelsecurity.DefaultRememberedSession
	PasswordResetSessionTTL         = panelsecurity.PasswordResetSessionTTL
	MFAPreauthSessionTTL            = panelsecurity.MFAPreauthSessionTTL
	SessionLastSeenDebounce         = panelsecurity.SessionLastSeenDebounce
	SessionLoginUserKey             = panelsecurity.SessionLoginUserKey
	SessionGenerationKey            = panelsecurity.SessionGenerationKey
	SessionUserIDKey                = panelsecurity.SessionUserIDKey
	SessionRefKey                   = panelsecurity.SessionRefKey
	SessionAuthStateKey             = panelsecurity.SessionAuthStateKey
	SessionAssuranceKey             = panelsecurity.SessionAssuranceKey
	SessionLastMFAAtKey             = panelsecurity.SessionLastMFAAtKey
	SessionLifetimePostureKey       = panelsecurity.SessionLifetimePostureKey
	SessionCredentialGenerationKey  = panelsecurity.SessionCredentialGenerationKey
	SessionMFAGenerationKey         = panelsecurity.SessionMFAGenerationKey
	SessionCreatedAtKey             = panelsecurity.SessionCreatedAtKey
	SessionAuthenticatedAtKey       = panelsecurity.SessionAuthenticatedAtKey
	SessionLastSeenAtKey            = panelsecurity.SessionLastSeenAtKey
	SessionIdleExpiresAtKey         = panelsecurity.SessionIdleExpiresAtKey
	SessionAbsoluteExpiresAtKey     = panelsecurity.SessionAbsoluteExpiresAtKey
	SessionRememberedExpiresAtKey   = panelsecurity.SessionRememberedExpiresAtKey
	SessionClientProvenanceKey      = panelsecurity.SessionClientProvenanceKey
	SessionClientPrefixKey          = panelsecurity.SessionClientPrefixKey
	SessionUserAgentHashKey         = panelsecurity.SessionUserAgentHashKey
	SessionDeviceLabelKey           = panelsecurity.SessionDeviceLabelKey
	SessionGenerationRevisionKey    = panelsecurity.SessionGenerationRevisionKey
	SessionPreAuthChallengeRevision = panelsecurity.SessionPreAuthChallengeRevision
)

type AuthenticationResult = panelsecurity.AuthenticationResult
type PanelAccountIdentity = panelsecurity.PanelAccountIdentity
type LoginSessionSpec = panelsecurity.LoginSessionSpec
type LoginSessionEstablishment = panelsecurity.LoginSessionEstablishment
type RealtimeSessionBinding = panelsecurity.RealtimeSessionBinding

func NewAuthenticationResult(id uint, username string, credentialGeneration, mfaGeneration uint64, authState, assurance string) AuthenticationResult {
	return panelsecurity.NewAuthenticationResult(id, username, credentialGeneration, mfaGeneration, authState, assurance)
}

func SessionGenerationRevision(generation string) string {
	return panelsecurity.SessionGenerationRevision(generation)
}

func SessionAuthenticationRevision(sessionRef string) string {
	return panelsecurity.SessionAuthenticationRevision(sessionRef)
}

func UserAgentDigest(userAgent string) string {
	return panelsecurity.UserAgentDigest(userAgent)
}
