package service

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	AuthStateAuthenticated = "authenticated"
	AuthStatePasswordReset = "password_reset"
	AuthStateMFAPending    = "mfa_pending"
	AuthStateMFARecovery   = "mfa_recovery"

	SessionStateActive  = "active"
	SessionStatePreAuth = "preauth"
	SessionStateRevoked = "revoked"

	AssurancePassword = "password"
	AssuranceMFA      = "mfa"
	AssuranceRecovery = "recovery"

	LifetimePostureBoundedV1       = "bounded_v1"
	LifetimePostureLegacyExplicit  = "legacy_explicit"
	LifetimePostureLegacyUnbounded = "legacy_unbounded"
	// LifetimePostureLegacy remains an alias for the historical configured
	// max-age posture used by compatibility callers.
	LifetimePostureLegacy = LifetimePostureLegacyExplicit

	DefaultSessionIdle       = 30 * time.Minute
	DefaultSessionAbsolute   = 12 * time.Hour
	DefaultRememberedSession = 7 * 24 * time.Hour
	PasswordResetSessionTTL  = 15 * time.Minute
	MFAPreauthSessionTTL     = 10 * time.Minute
	SessionLastSeenDebounce  = time.Minute
)

// Session value keys are shared by the API and the durable web session store.
// Values remain server-side; only the signed opaque session ID is in the cookie.
const (
	SessionLoginUserKey             = "LOGIN_USER"
	SessionGenerationKey            = "LOGIN_SESSION_GENERATION"
	SessionUserIDKey                = "__sui_security_user_id__"
	SessionRefKey                   = "__sui_security_ref__"
	SessionAuthStateKey             = "__sui_security_auth_state__"
	SessionAssuranceKey             = "__sui_security_assurance__"
	SessionLastMFAAtKey             = "__sui_security_last_mfa_at__"
	SessionLifetimePostureKey       = "__sui_security_lifetime_posture__"
	SessionCredentialGenerationKey  = "__sui_security_credential_generation__"
	SessionMFAGenerationKey         = "__sui_security_mfa_generation__"
	SessionCreatedAtKey             = "__sui_security_created_at__"
	SessionAuthenticatedAtKey       = "__sui_security_authenticated_at__"
	SessionLastSeenAtKey            = "__sui_security_last_seen_at__"
	SessionIdleExpiresAtKey         = "__sui_security_idle_expires_at__"
	SessionAbsoluteExpiresAtKey     = "__sui_security_absolute_expires_at__"
	SessionRememberedExpiresAtKey   = "__sui_security_remembered_expires_at__"
	SessionClientProvenanceKey      = "__sui_security_client_provenance__"
	SessionClientPrefixKey          = "__sui_security_client_prefix__"
	SessionUserAgentHashKey         = "__sui_security_user_agent_hash__"
	SessionDeviceLabelKey           = "__sui_security_device_label__"
	SessionGenerationRevisionKey    = "__sui_security_generation_revision__"
	SessionPreAuthChallengeRevision = "__sui_security_preauth_challenge_revision__"
)

type AuthenticationResult struct {
	User      modelUserIdentity
	AuthState string
	Assurance string
}

// modelUserIdentity keeps the HTTP boundary from serializing a password-bearing
// database model while retaining every fact required to mint a session.
type modelUserIdentity struct {
	ID                   uint
	Username             string
	CredentialGeneration uint64
	MFAGeneration        uint64
}

type LoginSessionSpec struct {
	UserID                   uint
	Username                 string
	AuthState                string
	Assurance                string
	LastMFAAt                int64
	LifetimePosture          string
	SessionGeneration        string
	CredentialGeneration     uint64
	MFAGeneration            uint64
	Remembered               bool
	ClientProvenance         string
	ClientPrefix             string
	UserAgentHash            string
	DeviceLabel              string
	PreAuthChallengeRevision string
	Now                      time.Time
	LegacyMaxAge             time.Duration
}

type RealtimeSessionBinding struct {
	UserID                    uint
	Username                  string
	SessionRef                string
	SessionGenerationRevision string
	CredentialGeneration      uint64
	MFAGeneration             uint64
}

func (r AuthenticationResult) UserID() uint { return r.User.ID }

func (r AuthenticationResult) Username() string { return r.User.Username }

func (r AuthenticationResult) CredentialGeneration() uint64 {
	return r.User.CredentialGeneration
}

func (r AuthenticationResult) MFAGeneration() uint64 { return r.User.MFAGeneration }

func NewAuthenticationResult(id uint, username string, credentialGeneration, mfaGeneration uint64, authState, assurance string) AuthenticationResult {
	return AuthenticationResult{
		User: modelUserIdentity{
			ID:                   id,
			Username:             username,
			CredentialGeneration: credentialGeneration,
			MFAGeneration:        mfaGeneration,
		},
		AuthState: authState,
		Assurance: assurance,
	}
}

func SessionGenerationRevision(generation string) string {
	if generation == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(generation))
	return hex.EncodeToString(sum[:])
}

func UserAgentDigest(userAgent string) string {
	sum := sha256.Sum256([]byte(userAgent))
	return hex.EncodeToString(sum[:])
}
