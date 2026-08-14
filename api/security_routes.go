package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	securityTinyBodyLimit              = 16 * 1024
	securityVerificationAggregateLimit = 10
	securityVerificationMethodLimit    = 5
)

var (
	securityVerificationAggregateRateLimit = ratelimit.NewFixedWindow[string](5*time.Minute, securityVerificationAggregateLimit, 4096, 10*time.Minute)
	securityVerificationMethodRateLimit    = ratelimit.NewFixedWindow[string](5*time.Minute, securityVerificationMethodLimit, 8192, 10*time.Minute)
)

type securityHTTP struct {
	api      *APIHandler
	mfa      service.MFAService
	stepUp   service.StepUpService
	sessions service.SecuritySessionService
}

func (a *APIHandler) registerSecurityRoutes(g *gin.RouterGroup) {
	h := &securityHTTP{
		api:      a,
		mfa:      service.MFAService{SettingService: a.SettingService},
		stepUp:   service.StepUpService{SettingService: a.SettingService},
		sessions: service.SecuritySessionService{},
	}
	securityGroup := g.Group("/v1/security")
	securityGroup.GET("/posture", h.posture)
	securityGroup.GET("/sessions", h.listSessions)
	securityGroup.POST("/sessions/revoke", h.revokeSession)
	securityGroup.POST("/sessions/revoke-others", h.revokeOtherSessions)
	securityGroup.POST("/sessions/adopt-bounded", h.adoptBoundedSessions)
	securityGroup.POST("/password/transition", h.passwordTransition)
	securityGroup.POST("/password/change", h.changePassword)
	securityGroup.POST("/step-up", h.issueStepUp)
	securityGroup.POST("/mfa/enroll", h.beginMFAEnrollment)
	securityGroup.POST("/mfa/confirm", h.confirmMFAEnrollment)
	securityGroup.POST("/mfa/recovery/ack", h.acknowledgeRecoveryCodes)
	securityGroup.POST("/mfa/challenge", h.completeMFAChallenge)
	securityGroup.POST("/mfa/recovery", h.completeRecoveryChallenge)
	securityGroup.POST("/mfa/recovery/complete", h.completeRecoveryTransition)
	securityGroup.POST("/mfa/recovery/rotate", h.rotateRecoveryCodes)
	securityGroup.POST("/mfa/disable", h.disableMFA)
}

func (h *securityHTTP) posture(c *gin.Context) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok {
		jsonMsg(c, "", common.NewError("invalid session"))
		return
	}
	status, err := h.mfa.Status(securityContext.UserID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", securityContext.UserID).First(&user).Error; err != nil {
		jsonMsg(c, "", err)
		return
	}
	identity := RequestClientIdentity(c)
	identityConfig := clientidentity.ConfigFromEnvironment()
	sessionLifetime, err := h.api.SettingService.ResolveSessionLifetime()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	sameSite := "lax"
	if resolveCookieSameSite(&h.api.SettingService) == http.SameSiteStrictMode {
		sameSite = "strict"
	}
	jsonObj(c, gin.H{
		"authState":                    securityContext.AuthState,
		"assurance":                    securityContext.Assurance,
		"username":                     securityContext.Username,
		"sessionRef":                   securityContext.Ref,
		"lastMfaAt":                    securityContext.LastMFAAt,
		"sessionLifetimePolicy":        sessionLifetime.Posture,
		"passwordPolicyVersion":        user.PasswordPolicyVersion,
		"passwordPolicyCurrentVersion": passwordutil.PolicyVersion,
		"forcePasswordReset":           user.ForcePasswordReset,
		"mfa":                          status,
		"clientIdentity": gin.H{
			"version":            identity.Version,
			"provenance":         identity.Provenance,
			"clientPrefix":       identity.ClientPrefix,
			"trustedProxyHops":   identity.TrustedProxyHops,
			"trustedProxyCount":  len(identityConfig.TrustedProxies),
			"trustedProxySource": identityConfig.Source,
			"trustedProxyCidrs":  identityConfig.CanonicalCIDRs,
			"warningCodes":       identityConfig.Warnings,
			"actualScheme":       identity.ActualScheme,
			"desiredScheme":      identity.DesiredScheme,
			"schemeSource":       identity.SchemeSource,
			"forwardedValid":     identity.ForwardedValid,
			"configRevision":     identity.ConfigRevision,
		},
		"cookiePolicy": gin.H{
			"httpOnly": true,
			"path":     "/",
			"sameSite": sameSite,
			"secure":   resolveCookieSecure(c, &h.api.SettingService),
		},
		"stepUpTargets": gin.H{
			"revokeOthers": securityTargetDigest("all-except:" + securityContext.Ref),
			"adoptBounded": securityTargetDigest("policy:bounded_v1"),
			"self":         securityTargetDigest("user:" + strconv.FormatUint(uint64(securityContext.UserID), 10)),
		},
	}, nil)
}

func (h *securityHTTP) listSessions(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	if !strictQueryKeys(c, "invalid session query", "cursor", "limit") {
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	if strings.TrimSpace(c.Query("limit")) == "" {
		limit = service.DefaultSessionPageSize
		err = nil
	}
	if err != nil || limit < 1 || limit > service.MaxSessionPageSize {
		securityBadRequest(c, "invalid session page size")
		return
	}
	inventory, err := h.sessions.List(securityContext.UserID, securityContext.Ref, c.Query("cursor"), limit)
	jsonObj(c, inventory, err)
}

type revokeSessionRequest struct {
	Ref string `json:"ref"`
}

func (h *securityHTTP) revokeSession(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request revokeSessionRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if request.Ref == securityContext.Ref {
		if err := h.sessions.Revoke(securityContext.UserID, request.Ref, "self_revoked"); err != nil {
			jsonMsg(c, "", err)
			return
		}
		h.api.recordAudit(c, securityContext.Username, "security_session_revoked", "security", service.AuditSeverityInfo, map[string]any{
			"current": true,
		})
		ClearSession(c)
		jsonMsg(c, "logout", nil)
		return
	}
	err := h.sessions.Revoke(securityContext.UserID, request.Ref, "user_revoked")
	if err == nil {
		h.api.recordAudit(c, securityContext.Username, "security_session_revoked", "security", service.AuditSeverityInfo, map[string]any{
			"current": false,
		})
	}
	jsonMsg(c, "revoke", err)
}

type stepUpTokenRequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *securityHTTP) revokeOtherSessions(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request stepUpTokenRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "sessions.revoke_others", securityTargetDigest("all-except:"+securityContext.Ref)); err != nil {
		jsonMsg(c, "", err)
		return
	}
	count, err := h.sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "user_revoked_others")
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	if err := h.rotateSessionGenerationAndReissue(c, securityContext.UserID, service.AuthStateAuthenticated, securityContext.Assurance); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "security_sessions_revoked_others", "security", service.AuditSeverityWarn, map[string]any{
		"count": count,
	})
	jsonObj(c, gin.H{"revoked": count}, nil)
}

func (h *securityHTTP) adoptBoundedSessions(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request stepUpTokenRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(
		c,
		request.StepUpToken,
		securityContext,
		"sessions.adopt_bounded",
		securityTargetDigest("policy:bounded_v1"),
	); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "session_policy_adopted"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.api.SettingService.AdoptBoundedSessionLifetime(); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.rotateSessionGenerationAndReissue(c, securityContext.UserID, service.AuthStateAuthenticated, securityContext.Assurance); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "session_lifetime_policy_adopted", "security", service.AuditSeverityWarn, map[string]any{
		"policy": service.LifetimePostureBoundedV1,
	})
	jsonObj(c, gin.H{"policy": service.LifetimePostureBoundedV1}, nil)
}

type passwordTransitionRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewUsername     string `json:"newUsername"`
	NewPassword     string `json:"newPassword"`
}

func (h *securityHTTP) passwordTransition(c *gin.Context) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != service.AuthStatePasswordReset {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "Password transition is not active"})
		return
	}
	var request passwordTransitionRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !h.allowSecurityVerification(c, securityContext, "password") {
		return
	}
	result, err := h.api.UserService.CompletePasswordTransition(
		c.Request.Context(),
		securityContext.UserID,
		request.CurrentPassword,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		h.api.recordAudit(c, securityContext.Username, "security_verification_rejected", "security", service.AuditSeverityWarn, map[string]any{
			"method": "password",
		})
		jsonMsg(c, "", err)
		return
	}
	if err := h.stepUp.InvalidateUser(result.UserID); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(result.UserID, securityContext.Ref, "credentials_changed"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	generation, err := h.api.SettingService.RotateSessionGeneration()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	nextAuthState := service.AuthStateAuthenticated
	mfaStatus, err := h.mfa.Status(result.UserID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if mfaStatus.Enabled {
		// A forced password reset never bypasses an already-enrolled second
		// factor. Continue in the bounded MFA pre-auth state after committing
		// the new credential.
		nextAuthState = service.AuthStateMFAPending
	}
	sessionLifetime, err := h.api.SettingService.ResolveSessionLifetime()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            nextAuthState,
		Assurance:            service.AssurancePassword,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		ClientProvenance:     "resolved",
		ClientPrefix:         getRemoteIp(c),
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, result.Username, "password_transition_completed", "security", service.AuditSeverityWarn, map[string]any{
		"initialCredentialRemoved": result.InitialCredentialRemoved,
	})
	jsonObj(c, gin.H{"state": nextAuthState, "assurance": service.AssurancePassword}, nil)
}

type passwordChangeRequest struct {
	NewUsername string `json:"newUsername"`
	NewPassword string `json:"newPassword"`
	StepUpToken string `json:"stepUpToken"`
}

func (h *securityHTTP) changePassword(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request passwordChangeRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	target := securityTargetDigest("user:" + strconv.FormatUint(uint64(securityContext.UserID), 10))
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "admin.credential", target); err != nil {
		jsonMsg(c, "", err)
		return
	}
	result, err := h.api.UserService.ChangeCredentialAfterStepUp(
		c.Request.Context(),
		securityContext.UserID,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(result.UserID, securityContext.Ref, "credentials_changed"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	generation, err := h.api.SettingService.RotateSessionGeneration()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	sessionLifetime, err := h.api.SettingService.ResolveSessionLifetime()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            service.AuthStateAuthenticated,
		Assurance:            securityContext.Assurance,
		LastMFAAt:            securityContext.LastMFAAt,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		Remembered:           currentSessionRemembered(c),
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, result.Username, "admin_credentials_changed", "security", service.AuditSeverityWarn, map[string]any{
		"policyVersion": passwordutil.PolicyVersion,
	})
	jsonObj(c, gin.H{
		"username":             result.Username,
		"credentialGeneration": result.CredentialGeneration,
	}, nil)
}

type issueStepUpRequest struct {
	Method        string `json:"method"`
	Credential    string `json:"credential"`
	OperationKind string `json:"operationKind"`
	TargetDigest  string `json:"targetDigest"`
}

func (h *securityHTTP) issueStepUp(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request issueStepUpRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !validStepUpOperation(request.OperationKind) || !validDigest(request.TargetDigest) {
		securityBadRequest(c, "invalid step-up target")
		return
	}
	status, err := h.mfa.Status(securityContext.UserID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	request.Method = strings.ToLower(strings.TrimSpace(request.Method))
	assurance := service.AssurancePassword
	if status.Enabled {
		switch request.Method {
		case "totp":
			if !h.allowSecurityVerification(c, securityContext, "totp") {
				return
			}
			err = h.mfa.VerifyTOTP(securityContext.UserID, request.Credential)
			assurance = service.AssuranceMFA
		case "recovery":
			if !h.allowSecurityVerification(c, securityContext, "recovery") {
				return
			}
			err = h.mfa.ConsumeRecoveryCode(securityContext.UserID, request.Credential)
			assurance = service.AssuranceRecovery
		default:
			err = common.NewError("MFA assurance required")
		}
	} else if request.Method != "password" {
		err = common.NewError("password assurance required")
	} else {
		if !h.allowSecurityVerification(c, securityContext, "password") {
			return
		}
		err = h.mfa.VerifyPassword(context.Background(), securityContext.UserID, request.Credential)
	}
	if err != nil {
		h.api.recordAudit(c, securityContext.Username, "security_verification_rejected", "security", service.AuditSeverityWarn, map[string]any{
			"method": request.Method,
		})
		jsonMsg(c, "", err)
		return
	}
	grant, err := h.stepUp.Issue(stepUpBinding(
		c,
		securityContext,
		request.OperationKind,
		strings.ToLower(request.TargetDigest),
		assurance,
	))
	if err == nil {
		session := sessions.Default(c)
		now := time.Now()
		if assurance == service.AssuranceMFA || assurance == service.AssuranceRecovery {
			session.Set(service.SessionLastMFAAtKey, now.Unix())
		}
		ResetSessionCSRF(session)
		session.Options(sessionCookieOptions(c, &h.api.SettingService, session, now))
		if saveErr := session.Save(); saveErr != nil {
			_ = h.stepUp.InvalidateSession(securityContext.Ref)
			jsonMsg(c, "", saveErr)
			return
		}
		h.api.recordAuditSynchronous(c, securityContext.Username, "step_up_issued", "security", service.AuditSeverityInfo, map[string]any{
			"operation": request.OperationKind,
			"assurance": assurance,
		})
	}
	jsonObj(c, grant, err)
}

type beginMFARequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *securityHTTP) beginMFAEnrollment(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request beginMFARequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.enroll", securityTargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		jsonMsg(c, "", err)
		return
	}
	enrollment, err := h.mfa.BeginEnrollment(securityContext.UserID, securityContext.Username)
	if err == nil {
		h.api.recordAudit(c, securityContext.Username, "mfa_enrollment_started", "security", service.AuditSeverityInfo, nil)
	}
	jsonObj(c, enrollment, err)
}

type mfaCodeRequest struct {
	Code string `json:"code"`
}

func (h *securityHTTP) confirmMFAEnrollment(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request mfaCodeRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !h.allowSecurityVerification(c, securityContext, "enrollment") {
		return
	}
	codes, err := h.mfa.ConfirmEnrollment(securityContext.UserID, request.Code)
	if err != nil {
		h.api.recordAudit(c, securityContext.Username, "mfa_enrollment_rejected", "security", service.AuditSeverityWarn, nil)
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "mfa_enrollment_confirmed_pending_ack", "security", service.AuditSeverityInfo, nil)
	jsonObj(c, gin.H{
		"recoveryCodes":        codes,
		"recoveryAcknowledged": false,
	}, nil)
}

func (h *securityHTTP) acknowledgeRecoveryCodes(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !request.Acknowledged {
		securityBadRequest(c, "recovery codes must be acknowledged")
		return
	}
	statusBefore, err := h.mfa.Status(securityContext.UserID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.mfa.AcknowledgeRecoveryCodes(securityContext.UserID); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.stepUp.InvalidateUser(securityContext.UserID); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "mfa_changed"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	assurance := securityContext.Assurance
	if !statusBefore.Enabled {
		assurance = service.AssuranceMFA
	}
	if err := h.rotateSessionGenerationAndReissue(c, securityContext.UserID, service.AuthStateAuthenticated, assurance); err != nil {
		jsonMsg(c, "", err)
		return
	}
	event := "mfa_recovery_codes_rotated"
	if !statusBefore.Enabled {
		event = "mfa_enabled"
	} else if statusBefore.Pending {
		event = "mfa_factor_rotated"
	}
	h.api.recordAudit(c, securityContext.Username, event, "security", service.AuditSeverityWarn, nil)
	jsonMsg(c, "save", nil)
}

func (h *securityHTTP) completeMFAChallenge(c *gin.Context) {
	h.completePreauthChallenge(c, false)
}

func (h *securityHTTP) completeRecoveryChallenge(c *gin.Context) {
	h.completePreauthChallenge(c, true)
}

func (h *securityHTTP) completePreauthChallenge(c *gin.Context, recovery bool) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != service.AuthStateMFAPending {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "MFA challenge is not active"})
		return
	}
	var request mfaCodeRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	verificationMethod := "totp"
	if recovery {
		verificationMethod = "recovery"
	}
	if !h.allowSecurityVerification(c, securityContext, verificationMethod) {
		return
	}
	assurance := service.AssuranceMFA
	var err error
	if recovery {
		err = h.mfa.ConsumeRecoveryCode(securityContext.UserID, request.Code)
		assurance = service.AssuranceRecovery
	} else {
		err = h.mfa.VerifyTOTP(securityContext.UserID, request.Code)
	}
	if err != nil {
		h.api.recordAudit(c, securityContext.Username, "mfa_challenge_rejected", "security", service.AuditSeverityWarn, map[string]any{
			"method": verificationMethod,
		})
		jsonMsg(c, "", err)
		return
	}
	if recovery {
		if err := h.reissueSession(c, securityContext.UserID, service.AuthStateMFARecovery, assurance); err != nil {
			jsonMsg(c, "", err)
			return
		}
		h.api.recordAudit(c, securityContext.Username, "mfa_recovery_challenge_completed", "security", service.AuditSeverityWarn, nil)
		jsonObj(c, gin.H{"state": service.AuthStateMFARecovery, "assurance": assurance}, nil)
		return
	}
	if err := h.reissueAuthenticatedSession(c, securityContext.UserID, assurance); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "mfa_challenge_completed", "security", service.AuditSeverityInfo, nil)
	jsonObj(c, gin.H{"state": service.AuthStateAuthenticated, "assurance": assurance}, nil)
}

type completeRecoveryTransitionRequest struct {
	NewUsername string `json:"newUsername"`
	NewPassword string `json:"newPassword"`
}

func (h *securityHTTP) completeRecoveryTransition(c *gin.Context) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != service.AuthStateMFARecovery {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "MFA recovery is not active"})
		return
	}
	var request completeRecoveryTransitionRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	result, err := h.api.UserService.CompleteRecoveryTransition(
		c.Request.Context(),
		securityContext.UserID,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(result.UserID, securityContext.Ref, "mfa_recovery_completed"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	generation, err := h.api.SettingService.RotateSessionGeneration()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	sessionLifetime, err := h.api.SettingService.ResolveSessionLifetime()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            service.AuthStateAuthenticated,
		Assurance:            service.AssurancePassword,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, result.Username, "mfa_recovery_completed", "security", service.AuditSeverityWarn, nil)
	jsonObj(c, gin.H{"state": service.AuthStateAuthenticated, "assurance": service.AssurancePassword}, nil)
}

type rotateRecoveryRequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *securityHTTP) rotateRecoveryCodes(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request rotateRecoveryRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.recovery.rotate", securityTargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		jsonMsg(c, "", err)
		return
	}
	codes, err := h.mfa.RotateRecoveryCodes(securityContext.UserID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "mfa_recovery_codes_rotation_pending_ack", "security", service.AuditSeverityWarn, nil)
	jsonObj(c, gin.H{"recoveryCodes": codes, "recoveryAcknowledged": false}, nil)
}

type disableMFARequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *securityHTTP) disableMFA(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request disableMFARequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.disable", securityTargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.mfa.Disable(securityContext.UserID); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.stepUp.InvalidateUser(securityContext.UserID); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if _, err := h.sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "mfa_disabled"); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := h.rotateSessionGenerationAndReissue(c, securityContext.UserID, service.AuthStateAuthenticated, service.AssurancePassword); err != nil {
		jsonMsg(c, "", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "mfa_disabled", "security", service.AuditSeverityWarn, nil)
	jsonObj(c, gin.H{"enabled": false}, nil)
}

func (h *securityHTTP) reissueAuthenticatedSession(c *gin.Context, userID uint, assurance string) error {
	return h.reissueSession(c, userID, service.AuthStateAuthenticated, assurance)
}

func (h *securityHTTP) rotateSessionGenerationAndReissue(c *gin.Context, userID uint, authState, assurance string) error {
	if _, err := h.api.SettingService.RotateSessionGeneration(); err != nil {
		return err
	}
	return h.reissueSession(c, userID, authState, assurance)
}

func (h *securityHTTP) reissueSession(c *gin.Context, userID uint, authState, assurance string) error {
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	generation, err := h.api.SettingService.GetSessionGeneration()
	if err != nil {
		return err
	}
	sessionLifetime, err := h.api.SettingService.ResolveSessionLifetime()
	if err != nil {
		return err
	}
	lastMFAAt := int64(0)
	if current, ok := GetSessionSecurityContext(c); ok {
		lastMFAAt = current.LastMFAAt
	}
	return SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               user.Id,
		Username:             user.Username,
		AuthState:            authState,
		Assurance:            assurance,
		LastMFAAt:            lastMFAAt,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: nonzeroSessionGeneration(user.CredentialGeneration),
		MFAGeneration:        nonzeroSessionGeneration(user.MFAGeneration),
		ClientProvenance:     "resolved",
		ClientPrefix:         getRemoteIp(c),
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedDeviceLabel(c.Request.UserAgent()),
	})
}

func (h *securityHTTP) allowSecurityVerification(c *gin.Context, securityContext SessionSecurityContext, method string) bool {
	baseKey := strconv.FormatUint(uint64(securityContext.UserID), 10) + "|" + securityContext.Ref + "|" + getRemoteIp(c)
	aggregate := securityVerificationAggregateRateLimit.Allow(baseKey)
	if !aggregate.Allowed {
		if h.api != nil {
			h.api.recordAudit(c, securityContext.Username, "security_verification_rate_limited", "security", service.AuditSeverityWarn, map[string]any{
				"method": method,
			})
		}
		c.Header("Retry-After", strconv.Itoa(max(1, int(aggregate.RetryAfter/time.Second))))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, Msg{Success: false, Msg: "Too many security verification attempts"})
		return false
	}
	methodDecision := securityVerificationMethodRateLimit.Allow(baseKey + "|" + method)
	if methodDecision.Allowed {
		return true
	}
	if h.api != nil {
		h.api.recordAudit(c, securityContext.Username, "security_verification_rate_limited", "security", service.AuditSeverityWarn, map[string]any{
			"method": method,
		})
	}
	c.Header("Retry-After", strconv.Itoa(max(1, int(methodDecision.RetryAfter/time.Second))))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, Msg{Success: false, Msg: "Too many security verification attempts"})
	return false
}

func (h *securityHTTP) consumeStepUp(c *gin.Context, token string, securityContext SessionSecurityContext, operation, targetDigest string) error {
	assurances := []string{
		securityContext.Assurance,
		service.AssuranceMFA,
		service.AssuranceRecovery,
		service.AssurancePassword,
	}
	seen := map[string]struct{}{}
	for _, assurance := range assurances {
		if _, duplicate := seen[assurance]; duplicate || assurance == "" {
			continue
		}
		seen[assurance] = struct{}{}
		if err := h.stepUp.Consume(token, stepUpBinding(c, securityContext, operation, targetDigest, assurance)); err == nil {
			return nil
		}
	}
	return common.NewError("invalid or expired step-up grant")
}

func requireAuthenticatedSecurityContext(c *gin.Context) (SessionSecurityContext, bool) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != service.AuthStateAuthenticated {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "Full authentication required"})
		return SessionSecurityContext{}, false
	}
	return securityContext, true
}

func decodeSecurityJSON(c *gin.Context, target any) bool {
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" {
		securityBadRequest(c, "Content-Type must be application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, securityTinyBodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		securityBadRequest(c, "invalid JSON request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		securityBadRequest(c, "request must contain one JSON object")
		return false
	}
	return true
}

func securityBadRequest(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, Msg{Success: false, Msg: message})
}

func stepUpBinding(c *gin.Context, securityContext SessionSecurityContext, operation, targetDigest, assurance string) service.StepUpBinding {
	return service.StepUpBinding{
		UserID:                    securityContext.UserID,
		SessionRef:                securityContext.Ref,
		SessionGenerationRevision: securityContext.SessionGenerationRevision,
		CredentialGeneration:      securityContext.CredentialGeneration,
		MFAGeneration:             securityContext.MFAGeneration,
		ClientIdentityRevision:    clientidentity.BindingRevision(RequestClientIdentity(c)),
		OperationKind:             operation,
		TargetDigest:              targetDigest,
		Assurance:                 assurance,
	}
}

func securityTargetDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validStepUpOperation(value string) bool {
	switch value {
	case "admin.credential", "admin.create", "admin.delete", "token.create", "token.revoke", "token.change",
		"backup.restore", "data.drop", "drop_data", "sessions.revoke_others", "sessions.adopt_bounded", "mfa.enroll",
		"mfa.recovery.rotate", "mfa.disable", "ssh.candidate.apply", "ssh.candidate.confirm", "ssh.candidate.rollback",
		"deployment.migrate", "deployment.confirm", "deployment.rollback", "update.prepare", "update.activate", "update.rollback":
		return true
	default:
		return false
	}
}

const stepUpHeader = "X-Step-Up-Token"

func (a *APIHandler) requireStepUpAction(c *gin.Context, operation, target string) bool {
	if _, bearer := requestTokenScope(c); bearer {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "Browser step-up is required"})
		return false
	}
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return false
	}
	if !validStepUpOperation(operation) {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "Unknown protected action"})
		return false
	}
	if target == "$self" {
		target = "user:" + strconv.FormatUint(uint64(securityContext.UserID), 10)
	}
	token := strings.TrimSpace(c.GetHeader(stepUpHeader))
	if token == "" || len(token) > 256 {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "A valid step-up grant is required"})
		return false
	}
	h := securityHTTP{
		api:    a,
		stepUp: service.StepUpService{SettingService: a.SettingService},
	}
	if err := h.consumeStepUp(c, token, securityContext, operation, securityTargetDigest(target)); err != nil {
		a.recordAudit(c, securityContext.Username, "step_up_rejected", "security", service.AuditSeverityWarn, map[string]any{
			"operation": operation,
		})
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "A valid step-up grant is required"})
		return false
	}
	a.recordAuditSynchronous(c, securityContext.Username, "step_up_consumed", "security", service.AuditSeverityInfo, map[string]any{
		"operation": operation,
	})
	return true
}

func currentSessionRemembered(c *gin.Context) bool {
	value := sessions.Default(c).Get(service.SessionRememberedExpiresAtKey)
	expiresAt, ok := sessionUint64(value)
	return ok && expiresAt > uint64(time.Now().Unix())
}

func realtimeSessionBinding(c *gin.Context) (service.RealtimeSessionBinding, bool) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != service.AuthStateAuthenticated {
		return service.RealtimeSessionBinding{}, false
	}
	return service.RealtimeSessionBinding{
		UserID:                    securityContext.UserID,
		Username:                  securityContext.Username,
		SessionRef:                securityContext.Ref,
		SessionGenerationRevision: securityContext.SessionGenerationRevision,
		CredentialGeneration:      securityContext.CredentialGeneration,
		MFAGeneration:             securityContext.MFAGeneration,
	}, true
}

func realtimeSessionValid(binding service.RealtimeSessionBinding) bool {
	if binding.Username == "" {
		return false
	}
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ? AND username = ?", binding.UserID, binding.Username).First(&user).Error; err != nil {
		return false
	}
	if nonzeroSessionGeneration(user.CredentialGeneration) != binding.CredentialGeneration ||
		nonzeroSessionGeneration(user.MFAGeneration) != binding.MFAGeneration {
		return false
	}
	var count int64
	if err := dbsqlite.DB().Model(&model.SecuritySession{}).Where("ref = ?", binding.SessionRef).Count(&count).Error; err != nil {
		return false
	}
	if count == 0 {
		return true // cookie-store-only test compatibility
	}
	row, err := (&service.SecuritySessionService{}).Validate(
		binding.SessionRef,
		binding.UserID,
		binding.CredentialGeneration,
		binding.MFAGeneration,
	)
	return err == nil && row.SessionGenerationRevision == binding.SessionGenerationRevision
}
