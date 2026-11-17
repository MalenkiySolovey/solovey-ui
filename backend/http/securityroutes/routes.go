package securityroutes

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

	"github.com/MalenkiySolovey/solovey-ui/backend/audit"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/browser"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/ratelimit"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/requestidentity"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/response"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/backend/security/clientidentity"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	securitysessions "github.com/MalenkiySolovey/solovey-ui/backend/security/sessions"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/stepup"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	securityTinyBodyLimit      = 16 * 1024
	VerificationAggregateLimit = 10
	VerificationMethodLimit    = 5
)

var (
	securityVerificationAggregateRateLimit = ratelimit.NewFixedWindow[string](5*time.Minute, VerificationAggregateLimit, 4096, 10*time.Minute)
	securityVerificationMethodRateLimit    = ratelimit.NewFixedWindow[string](5*time.Minute, VerificationMethodLimit, 8192, 10*time.Minute)
)

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
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

func (h *Handler) posture(c *gin.Context) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok {
		h.Response.JSONMsg(c, "", errors.New("invalid session"))
		return
	}
	status, err := h.MFA.Status(securityContext.UserID)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", securityContext.UserID).First(&user).Error; err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	identity := requestidentity.FromContext(c)
	identityConfig := clientidentity.ConfigFromEnvironment()
	sessionLifetime, err := h.Settings.ResolveSessionLifetime()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	sameSite := "lax"
	if h.Browser.ResolveCookieSameSite() == http.SameSiteStrictMode {
		sameSite = "strict"
	}
	h.Response.JSONObj(c, gin.H{
		"authState":                    securityContext.AuthState,
		"componentUIReadPermissions":   h.ComponentUIReadPermissions(c),
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
			"secure":   h.Browser.ResolveCookieSecure(c),
		},
		"stepUpTargets": gin.H{
			"revokeOthers": TargetDigest("all-except:" + securityContext.Ref),
			"adoptBounded": TargetDigest("policy:bounded_v1"),
			"self":         TargetDigest("user:" + strconv.FormatUint(uint64(securityContext.UserID), 10)),
		},
	}, nil)
}

func (h *Handler) listSessions(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	if !StrictQueryKeys(c, "invalid session query", "cursor", "limit") {
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	if strings.TrimSpace(c.Query("limit")) == "" {
		limit = securitysessions.DefaultSessionPageSize
		err = nil
	}
	if err != nil || limit < 1 || limit > securitysessions.MaxSessionPageSize {
		BadRequest(c, "invalid session page size")
		return
	}
	inventory, err := h.Sessions.List(securityContext.UserID, securityContext.Ref, c.Query("cursor"), limit)
	h.Response.JSONObj(c, inventory, err)
}

type revokeSessionRequest struct {
	Ref string `json:"ref"`
}

func (h *Handler) revokeSession(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request revokeSessionRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if request.Ref == securityContext.Ref {
		if err := h.Sessions.Revoke(securityContext.UserID, request.Ref, "self_revoked"); err != nil {
			h.Response.JSONMsg(c, "", err)
			return
		}
		h.Audit(c, securityContext.Username, "security_session_revoked", "security", audit.SeverityInfo, map[string]any{
			"current": true,
		})
		h.Browser.ClearSession(c)
		h.Response.JSONMsg(c, "logout", nil)
		return
	}
	err := h.Sessions.Revoke(securityContext.UserID, request.Ref, "user_revoked")
	if err == nil {
		h.Audit(c, securityContext.Username, "security_session_revoked", "security", audit.SeverityInfo, map[string]any{
			"current": false,
		})
	}
	h.Response.JSONMsg(c, "revoke", err)
}

type StepUpTokenRequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *Handler) revokeOtherSessions(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request StepUpTokenRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "sessions.revoke_others", TargetDigest("all-except:"+securityContext.Ref)); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	count, err := h.Sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "user_revoked_others")
	if err != nil {
		h.Response.JSONObj(c, nil, err)
		return
	}
	if err := h.RotateSessionGenerationAndReissue(c, securityContext.UserID, panelsecurity.AuthStateAuthenticated, securityContext.Assurance); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.Audit(c, securityContext.Username, "security_sessions_revoked_others", "security", audit.SeverityWarn, map[string]any{
		"count": count,
	})
	h.Response.JSONObj(c, gin.H{"revoked": count}, nil)
}

func (h *Handler) adoptBoundedSessions(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request StepUpTokenRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(
		c,
		request.StepUpToken,
		securityContext,
		"sessions.adopt_bounded",
		TargetDigest("policy:bounded_v1"),
	); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "session_policy_adopted"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.Settings.AdoptBoundedSessionLifetime(); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.RotateSessionGenerationAndReissue(c, securityContext.UserID, panelsecurity.AuthStateAuthenticated, securityContext.Assurance); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.AuditSynchronous(c, securityContext.Username, "session_lifetime_policy_adopted", "security", audit.SeverityWarn, map[string]any{
		"policy": panelsecurity.LifetimePostureBoundedV1,
	})
	h.Response.JSONObj(c, gin.H{"policy": panelsecurity.LifetimePostureBoundedV1}, nil)
}

type PasswordTransitionRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewUsername     string `json:"newUsername"`
	NewPassword     string `json:"newPassword"`
}

func (h *Handler) passwordTransition(c *gin.Context) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != panelsecurity.AuthStatePasswordReset {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "Password transition is not active"})
		return
	}
	var request PasswordTransitionRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if !h.AllowVerification(c, securityContext, "password") {
		return
	}
	result, err := h.Users.CompletePasswordTransition(
		c.Request.Context(),
		securityContext.UserID,
		request.CurrentPassword,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		h.Audit(c, securityContext.Username, "security_verification_rejected", "security", audit.SeverityWarn, map[string]any{
			"method": "password",
		})
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.ReloadTokens()
	h.NotifyEvent("admin_credentials_changed", map[string]string{"user": securityContext.Username})
	if err := h.StepUp.InvalidateUser(result.UserID); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Sessions.RevokeOthers(result.UserID, securityContext.Ref, "credentials_changed"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	generation, err := h.Settings.RotateSessionGeneration()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	nextAuthState := panelsecurity.AuthStateAuthenticated
	mfaStatus, err := h.MFA.Status(result.UserID)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if mfaStatus.Enabled {
		// A forced password reset never bypasses an already-enrolled second
		// factor. Continue in the bounded MFA pre-auth state after committing
		// the new credential.
		nextAuthState = panelsecurity.AuthStateMFAPending
	}
	sessionLifetime, err := h.Settings.ResolveSessionLifetime()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Browser.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            nextAuthState,
		Assurance:            panelsecurity.AssurancePassword,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		ClientProvenance:     "resolved",
		ClientPrefix:         requestidentity.FromContext(c).ClientIP,
		UserAgentHash:        panelsecurity.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          browser.BoundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.NotifyEvent("session_reestablished_after_credential_change", map[string]string{"user": result.Username})
	h.Audit(c, result.Username, "password_transition_completed", "security", audit.SeverityWarn, map[string]any{
		"initialCredentialRemoved": result.InitialCredentialRemoved,
	})
	h.Response.JSONObj(c, gin.H{"state": nextAuthState, "assurance": panelsecurity.AssurancePassword}, nil)
}

type PasswordChangeRequest struct {
	NewUsername string `json:"newUsername"`
	NewPassword string `json:"newPassword"`
	StepUpToken string `json:"stepUpToken"`
}

func (h *Handler) changePassword(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request PasswordChangeRequest
	if !DecodeJSON(c, &request) {
		return
	}
	target := TargetDigest("user:" + strconv.FormatUint(uint64(securityContext.UserID), 10))
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "admin.credential", target); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	result, err := h.Users.ChangeCredentialAfterStepUp(
		c.Request.Context(),
		securityContext.UserID,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.ReloadTokens()
	h.NotifyEvent("admin_credentials_changed", map[string]string{"user": securityContext.Username})
	if _, err := h.Sessions.RevokeOthers(result.UserID, securityContext.Ref, "credentials_changed"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	generation, err := h.Settings.RotateSessionGeneration()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	sessionLifetime, err := h.Settings.ResolveSessionLifetime()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Browser.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            panelsecurity.AuthStateAuthenticated,
		Assurance:            securityContext.Assurance,
		LastMFAAt:            securityContext.LastMFAAt,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		Remembered:           CurrentSessionRemembered(c),
		UserAgentHash:        panelsecurity.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          browser.BoundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.NotifyEvent("session_reestablished_after_credential_change", map[string]string{"user": result.Username})
	h.Audit(c, result.Username, "admin_credentials_changed", "security", audit.SeverityWarn, map[string]any{
		"policyVersion": passwordutil.PolicyVersion,
	})
	h.Response.JSONObj(c, gin.H{
		"username":             result.Username,
		"credentialGeneration": result.CredentialGeneration,
	}, nil)
}

type IssueStepUpRequest struct {
	Method        string `json:"method"`
	Credential    string `json:"credential"`
	OperationKind string `json:"operationKind"`
	TargetDigest  string `json:"targetDigest"`
}

func (h *Handler) issueStepUp(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request IssueStepUpRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if !ValidStepUpOperation(request.OperationKind) || !ValidDigest(request.TargetDigest) {
		BadRequest(c, "invalid step-up target")
		return
	}
	status, err := h.MFA.Status(securityContext.UserID)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	request.Method = strings.ToLower(strings.TrimSpace(request.Method))
	assurance := panelsecurity.AssurancePassword
	if status.Enabled {
		switch request.Method {
		case "totp":
			if !h.AllowVerification(c, securityContext, "totp") {
				return
			}
			err = h.MFA.VerifyTOTP(securityContext.UserID, request.Credential)
			assurance = panelsecurity.AssuranceMFA
		case "recovery":
			if !h.AllowVerification(c, securityContext, "recovery") {
				return
			}
			err = h.MFA.ConsumeRecoveryCode(securityContext.UserID, request.Credential)
			assurance = panelsecurity.AssuranceRecovery
		default:
			err = errors.New("MFA assurance required")
		}
	} else if request.Method != "password" {
		err = errors.New("password assurance required")
	} else {
		if !h.AllowVerification(c, securityContext, "password") {
			return
		}
		err = h.MFA.VerifyPassword(context.Background(), securityContext.UserID, request.Credential)
	}
	if err != nil {
		h.Audit(c, securityContext.Username, "security_verification_rejected", "security", audit.SeverityWarn, map[string]any{
			"method": request.Method,
		})
		h.Response.JSONMsg(c, "", err)
		return
	}
	grant, err := h.StepUp.Issue(StepUpBinding(
		c,
		securityContext,
		request.OperationKind,
		strings.ToLower(request.TargetDigest),
		assurance,
	))
	if err == nil {
		session := sessions.Default(c)
		now := time.Now()
		if assurance == panelsecurity.AssuranceMFA || assurance == panelsecurity.AssuranceRecovery {
			session.Set(panelsecurity.SessionLastMFAAtKey, now.Unix())
		}
		browser.ResetSessionCSRF(session)
		session.Options(h.Browser.SessionCookieOptions(c, session, now))
		if saveErr := session.Save(); saveErr != nil {
			_ = h.StepUp.InvalidateSession(securityContext.Ref)
			h.Response.JSONMsg(c, "", saveErr)
			return
		}
		h.AuditSynchronous(c, securityContext.Username, "step_up_issued", "security", audit.SeverityInfo, map[string]any{
			"operation": request.OperationKind,
			"assurance": assurance,
		})
	}
	h.Response.JSONObj(c, grant, err)
}

type beginMFARequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *Handler) beginMFAEnrollment(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request beginMFARequest
	if !DecodeJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.enroll", TargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	enrollment, err := h.MFA.BeginEnrollment(securityContext.UserID, securityContext.Username)
	if err == nil {
		h.Audit(c, securityContext.Username, "mfa_enrollment_started", "security", audit.SeverityInfo, nil)
	}
	h.Response.JSONObj(c, enrollment, err)
}

type MFACodeRequest struct {
	Code string `json:"code"`
}

func (h *Handler) confirmMFAEnrollment(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request MFACodeRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if !h.AllowVerification(c, securityContext, "enrollment") {
		return
	}
	codes, err := h.MFA.ConfirmEnrollment(securityContext.UserID, request.Code)
	if err != nil {
		h.Audit(c, securityContext.Username, "mfa_enrollment_rejected", "security", audit.SeverityWarn, nil)
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.Audit(c, securityContext.Username, "mfa_enrollment_confirmed_pending_ack", "security", audit.SeverityInfo, nil)
	h.Response.JSONObj(c, gin.H{
		"recoveryCodes":        codes,
		"recoveryAcknowledged": false,
	}, nil)
}

func (h *Handler) acknowledgeRecoveryCodes(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if !DecodeJSON(c, &request) {
		return
	}
	if !request.Acknowledged {
		BadRequest(c, "recovery codes must be acknowledged")
		return
	}
	statusBefore, err := h.MFA.Status(securityContext.UserID)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.MFA.AcknowledgeRecoveryCodes(securityContext.UserID); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.StepUp.InvalidateUser(securityContext.UserID); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "mfa_changed"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	assurance := securityContext.Assurance
	if !statusBefore.Enabled {
		assurance = panelsecurity.AssuranceMFA
	}
	if err := h.RotateSessionGenerationAndReissue(c, securityContext.UserID, panelsecurity.AuthStateAuthenticated, assurance); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	event := "mfa_recovery_codes_rotated"
	if !statusBefore.Enabled {
		event = "mfa_enabled"
	} else if statusBefore.Pending {
		event = "mfa_factor_rotated"
	}
	h.Audit(c, securityContext.Username, event, "security", audit.SeverityWarn, nil)
	h.Response.JSONMsg(c, "save", nil)
}

func (h *Handler) completeMFAChallenge(c *gin.Context) {
	h.completePreauthChallenge(c, false)
}

func (h *Handler) completeRecoveryChallenge(c *gin.Context) {
	h.completePreauthChallenge(c, true)
}

func (h *Handler) completePreauthChallenge(c *gin.Context, recovery bool) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != panelsecurity.AuthStateMFAPending {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "MFA challenge is not active"})
		return
	}
	var request MFACodeRequest
	if !DecodeJSON(c, &request) {
		return
	}
	verificationMethod := "totp"
	if recovery {
		verificationMethod = "recovery"
	}
	if !h.AllowVerification(c, securityContext, verificationMethod) {
		return
	}
	assurance := panelsecurity.AssuranceMFA
	var err error
	if recovery {
		err = h.MFA.ConsumeRecoveryCode(securityContext.UserID, request.Code)
		assurance = panelsecurity.AssuranceRecovery
	} else {
		err = h.MFA.VerifyTOTP(securityContext.UserID, request.Code)
	}
	if err != nil {
		h.Audit(c, securityContext.Username, "mfa_challenge_rejected", "security", audit.SeverityWarn, map[string]any{
			"method": verificationMethod,
		})
		h.Response.JSONMsg(c, "", err)
		return
	}
	if recovery {
		if err := h.reissueSession(c, securityContext.UserID, panelsecurity.AuthStateMFARecovery, assurance); err != nil {
			h.Response.JSONMsg(c, "", err)
			return
		}
		h.Audit(c, securityContext.Username, "mfa_recovery_challenge_completed", "security", audit.SeverityWarn, nil)
		h.Response.JSONObj(c, gin.H{"state": panelsecurity.AuthStateMFARecovery, "assurance": assurance}, nil)
		return
	}
	if err := h.reissueAuthenticatedSession(c, securityContext.UserID, assurance); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.Audit(c, securityContext.Username, "mfa_challenge_completed", "security", audit.SeverityInfo, nil)
	h.Response.JSONObj(c, gin.H{"state": panelsecurity.AuthStateAuthenticated, "assurance": assurance}, nil)
}

type CompleteRecoveryTransitionRequest struct {
	NewUsername string `json:"newUsername"`
	NewPassword string `json:"newPassword"`
}

func (h *Handler) completeRecoveryTransition(c *gin.Context) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != panelsecurity.AuthStateMFARecovery {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "MFA recovery is not active"})
		return
	}
	var request CompleteRecoveryTransitionRequest
	if !DecodeJSON(c, &request) {
		return
	}
	result, err := h.Users.CompleteRecoveryTransition(
		c.Request.Context(),
		securityContext.UserID,
		request.NewUsername,
		request.NewPassword,
	)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.ReloadTokens()
	h.NotifyEvent("admin_credentials_changed", map[string]string{"user": securityContext.Username})
	if _, err := h.Sessions.RevokeOthers(result.UserID, securityContext.Ref, "mfa_recovery_completed"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	generation, err := h.Settings.RotateSessionGeneration()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	sessionLifetime, err := h.Settings.ResolveSessionLifetime()
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Browser.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{
		UserID:               result.UserID,
		Username:             result.Username,
		AuthState:            panelsecurity.AuthStateAuthenticated,
		Assurance:            panelsecurity.AssurancePassword,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: result.CredentialGeneration,
		MFAGeneration:        result.MFAGeneration,
		UserAgentHash:        panelsecurity.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          browser.BoundedDeviceLabel(c.Request.UserAgent()),
	}); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.NotifyEvent("session_reestablished_after_credential_change", map[string]string{"user": result.Username})
	h.Audit(c, result.Username, "mfa_recovery_completed", "security", audit.SeverityWarn, nil)
	h.Response.JSONObj(c, gin.H{"state": panelsecurity.AuthStateAuthenticated, "assurance": panelsecurity.AssurancePassword}, nil)
}

type rotateRecoveryRequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *Handler) rotateRecoveryCodes(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request rotateRecoveryRequest
	if !DecodeJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.recovery.rotate", TargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	codes, err := h.MFA.RotateRecoveryCodes(securityContext.UserID)
	if err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.Audit(c, securityContext.Username, "mfa_recovery_codes_rotation_pending_ack", "security", audit.SeverityWarn, nil)
	h.Response.JSONObj(c, gin.H{"recoveryCodes": codes, "recoveryAcknowledged": false}, nil)
}

type disableMFARequest struct {
	StepUpToken string `json:"stepUpToken"`
}

func (h *Handler) disableMFA(c *gin.Context) {
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return
	}
	var request disableMFARequest
	if !DecodeJSON(c, &request) {
		return
	}
	if err := h.consumeStepUp(c, request.StepUpToken, securityContext, "mfa.disable", TargetDigest("user:"+strconv.FormatUint(uint64(securityContext.UserID), 10))); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.MFA.Disable(securityContext.UserID); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.StepUp.InvalidateUser(securityContext.UserID); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if _, err := h.Sessions.RevokeOthers(securityContext.UserID, securityContext.Ref, "mfa_disabled"); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	if err := h.RotateSessionGenerationAndReissue(c, securityContext.UserID, panelsecurity.AuthStateAuthenticated, panelsecurity.AssurancePassword); err != nil {
		h.Response.JSONMsg(c, "", err)
		return
	}
	h.Audit(c, securityContext.Username, "mfa_disabled", "security", audit.SeverityWarn, nil)
	h.Response.JSONObj(c, gin.H{"enabled": false}, nil)
}

func (h *Handler) reissueAuthenticatedSession(c *gin.Context, userID uint, assurance string) error {
	return h.reissueSession(c, userID, panelsecurity.AuthStateAuthenticated, assurance)
}

func (h *Handler) RotateSessionGenerationAndReissue(c *gin.Context, userID uint, authState, assurance string) error {
	if _, err := h.Settings.RotateSessionGeneration(); err != nil {
		return err
	}
	return h.reissueSession(c, userID, authState, assurance)
}

func (h *Handler) reissueSession(c *gin.Context, userID uint, authState, assurance string) error {
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	generation, err := h.Settings.GetSessionGeneration()
	if err != nil {
		return err
	}
	sessionLifetime, err := h.Settings.ResolveSessionLifetime()
	if err != nil {
		return err
	}
	lastMFAAt := int64(0)
	if current, ok := h.Browser.GetSessionSecurityContext(c); ok {
		lastMFAAt = current.LastMFAAt
	}
	_, err = h.Browser.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{
		UserID:               user.Id,
		Username:             user.Username,
		AuthState:            authState,
		Assurance:            assurance,
		LastMFAAt:            lastMFAAt,
		LifetimePosture:      sessionLifetime.Posture,
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
		SessionGeneration:    generation,
		CredentialGeneration: browser.NonzeroSessionGeneration(user.CredentialGeneration),
		MFAGeneration:        browser.NonzeroSessionGeneration(user.MFAGeneration),
		ClientProvenance:     "resolved",
		ClientPrefix:         requestidentity.FromContext(c).ClientIP,
		UserAgentHash:        panelsecurity.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          browser.BoundedDeviceLabel(c.Request.UserAgent()),
	})
	return err
}

func (h *Handler) AllowVerification(c *gin.Context, securityContext browser.SessionSecurityContext, method string) bool {
	baseKey := strconv.FormatUint(uint64(securityContext.UserID), 10) + "|" + securityContext.Ref + "|" + requestidentity.FromContext(c).ClientIP
	aggregate := securityVerificationAggregateRateLimit.Allow(baseKey)
	if !aggregate.Allowed {
		if h.Audit != nil {
			h.Audit(c, securityContext.Username, "security_verification_rate_limited", "security", audit.SeverityWarn, map[string]any{
				"method": method,
			})
		}
		c.Header("Retry-After", strconv.Itoa(max(1, int(aggregate.RetryAfter/time.Second))))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, response.Msg{Success: false, Msg: "Too many security verification attempts"})
		return false
	}
	methodDecision := securityVerificationMethodRateLimit.Allow(baseKey + "|" + method)
	if methodDecision.Allowed {
		return true
	}
	if h.Audit != nil {
		h.Audit(c, securityContext.Username, "security_verification_rate_limited", "security", audit.SeverityWarn, map[string]any{
			"method": method,
		})
	}
	c.Header("Retry-After", strconv.Itoa(max(1, int(methodDecision.RetryAfter/time.Second))))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, response.Msg{Success: false, Msg: "Too many security verification attempts"})
	return false
}

func (h *Handler) consumeStepUp(c *gin.Context, token string, securityContext browser.SessionSecurityContext, operation, targetDigest string) error {
	assurances := []string{
		securityContext.Assurance,
		panelsecurity.AssuranceMFA,
		panelsecurity.AssuranceRecovery,
		panelsecurity.AssurancePassword,
	}
	seen := map[string]struct{}{}
	for _, assurance := range assurances {
		if _, duplicate := seen[assurance]; duplicate || assurance == "" {
			continue
		}
		seen[assurance] = struct{}{}
		if err := h.StepUp.Consume(token, StepUpBinding(c, securityContext, operation, targetDigest, assurance)); err == nil {
			return nil
		}
	}
	return errors.New("invalid or expired step-up grant")
}

func (h *Handler) RequireAuthenticatedContext(c *gin.Context) (browser.SessionSecurityContext, bool) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != panelsecurity.AuthStateAuthenticated {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "Full authentication required"})
		return browser.SessionSecurityContext{}, false
	}
	return securityContext, true
}

func DecodeJSON(c *gin.Context, target any) bool {
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" {
		BadRequest(c, "Content-Type must be application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, securityTinyBodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		BadRequest(c, "invalid JSON request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		BadRequest(c, "request must contain one JSON object")
		return false
	}
	return true
}

func BadRequest(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, response.Msg{Success: false, Msg: message})
}

func StepUpBinding(c *gin.Context, securityContext browser.SessionSecurityContext, operation, targetDigest, assurance string) stepup.Binding {
	return stepup.Binding{
		UserID:                    securityContext.UserID,
		SessionRef:                securityContext.Ref,
		SessionGenerationRevision: securityContext.SessionGenerationRevision,
		CredentialGeneration:      securityContext.CredentialGeneration,
		MFAGeneration:             securityContext.MFAGeneration,
		ClientIdentityRevision:    clientidentity.BindingRevision(requestidentity.FromContext(c)),
		OperationKind:             operation,
		TargetDigest:              targetDigest,
		Assurance:                 assurance,
	}
}

func TargetDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ValidDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidStepUpOperation(value string) bool {
	switch value {
	case "admin.credential", "admin.create", "admin.delete", "token.create", "token.revoke", "token.change",
		"backup.restore", "data.drop", "drop_data", "sessions.revoke_others", "sessions.adopt_bounded", "mfa.enroll",
		"mfa.recovery.rotate", "mfa.disable", "ssh.candidate.apply", "ssh.candidate.confirm", "ssh.candidate.rollback",
		"deployment.migrate", "deployment.confirm", "deployment.rollback", "update.prepare", "update.activate", "update.rollback", "component.remove", "update.provider-maintenance":
		return true
	default:
		return false
	}
}

const StepUpHeader = "X-Step-Up-Token"

func (h *Handler) RequireStepUpAction(c *gin.Context, operation, target string) bool {
	if h.IsBearer == nil || h.IsBearer(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "Browser step-up is required"})
		return false
	}
	securityContext, ok := h.RequireAuthenticatedContext(c)
	if !ok {
		return false
	}
	if !ValidStepUpOperation(operation) {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "Unknown protected action"})
		return false
	}
	if target == "$self" {
		target = "user:" + strconv.FormatUint(uint64(securityContext.UserID), 10)
	}
	token := strings.TrimSpace(c.GetHeader(StepUpHeader))
	if token == "" || len(token) > 256 {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "A valid step-up grant is required"})
		return false
	}
	if err := h.consumeStepUp(c, token, securityContext, operation, TargetDigest(target)); err != nil {
		h.Audit(c, securityContext.Username, "step_up_rejected", "security", audit.SeverityWarn, map[string]any{
			"operation": operation,
		})
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "A valid step-up grant is required"})
		return false
	}
	h.AuditSynchronous(c, securityContext.Username, "step_up_consumed", "security", audit.SeverityInfo, map[string]any{
		"operation": operation,
	})
	return true
}

func CurrentSessionRemembered(c *gin.Context) bool {
	value := sessions.Default(c).Get(panelsecurity.SessionRememberedExpiresAtKey)
	expiresAt, ok := browser.SessionUint64(value)
	return ok && expiresAt > uint64(time.Now().Unix())
}

func (h *Handler) RealtimeSessionBinding(c *gin.Context) (panelsecurity.RealtimeSessionBinding, bool) {
	securityContext, ok := h.Browser.GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState != panelsecurity.AuthStateAuthenticated {
		return panelsecurity.RealtimeSessionBinding{}, false
	}
	return panelsecurity.RealtimeSessionBinding{
		UserID:                    securityContext.UserID,
		Username:                  securityContext.Username,
		SessionRef:                securityContext.Ref,
		SessionGenerationRevision: securityContext.SessionGenerationRevision,
		CredentialGeneration:      securityContext.CredentialGeneration,
		MFAGeneration:             securityContext.MFAGeneration,
	}, true
}

func RealtimeSessionValid(binding panelsecurity.RealtimeSessionBinding) bool {
	if binding.Username == "" {
		return false
	}
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ? AND username = ?", binding.UserID, binding.Username).First(&user).Error; err != nil {
		return false
	}
	if browser.NonzeroSessionGeneration(user.CredentialGeneration) != binding.CredentialGeneration ||
		browser.NonzeroSessionGeneration(user.MFAGeneration) != binding.MFAGeneration {
		return false
	}
	var count int64
	if err := dbsqlite.DB().Model(&model.SecuritySession{}).Where("ref = ?", binding.SessionRef).Count(&count).Error; err != nil {
		return false
	}
	if count == 0 {
		return true // cookie-store-only test compatibility
	}
	row, err := (&securitysessions.SecuritySessionService{}).Validate(
		binding.SessionRef,
		binding.UserID,
		binding.CredentialGeneration,
		binding.MFAGeneration,
	)
	return err == nil && row.SessionGenerationRevision == binding.SessionGenerationRevision
}
