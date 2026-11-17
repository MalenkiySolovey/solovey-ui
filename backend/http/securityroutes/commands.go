// Package securityroutes owns Panel session, credential, MFA and single-use grant HTTP admission.
package securityroutes

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/browser"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/response"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/accounts"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/mfa"
	securitysessions "github.com/MalenkiySolovey/solovey-ui/backend/security/sessions"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/stepup"
	"github.com/gin-gonic/gin"
)

type Settings interface {
	browser.Settings
	ResolveSessionLifetime() (panelsecurity.SessionLifetime, error)
	RotateSessionGeneration() (string, error)
	AdoptBoundedSessionLifetime() error
}
type Credentials interface {
	CompletePasswordTransition(context.Context, uint, string, string, string) (accounts.PasswordTransitionResult, error)
	ChangeCredentialAfterStepUp(context.Context, uint, string, string) (accounts.PasswordTransitionResult, error)
	CompleteRecoveryTransition(context.Context, uint, string, string) (accounts.PasswordTransitionResult, error)
}
type MFACommands interface {
	Status(uint) (mfa.MFAStatus, error)
	BeginEnrollment(uint, string) (mfa.MFAEnrollment, error)
	ConfirmEnrollment(uint, string) ([]string, error)
	AcknowledgeRecoveryCodes(uint) error
	VerifyTOTP(uint, string) error
	ConsumeRecoveryCode(uint, string) error
	RotateRecoveryCodes(uint) ([]string, error)
	Disable(uint) error
	VerifyPassword(context.Context, uint, string) error
}
type StepUpCommands interface {
	Issue(stepup.Binding) (stepup.IssuedGrant, error)
	Consume(string, stepup.Binding) error
	InvalidateUser(uint) error
	InvalidateSession(string) error
}
type SessionCommands interface {
	List(uint, string, string, int) (securitysessions.SessionInventory, error)
	Revoke(uint, string, string) error
	RevokeOthers(uint, string, string) (int64, error)
}
type Handler struct {
	Settings                   Settings
	Users                      Credentials
	MFA                        MFACommands
	StepUp                     StepUpCommands
	Sessions                   SessionCommands
	Browser                    *browser.Service
	Response                   response.Writer
	Audit                      func(*gin.Context, string, string, string, string, map[string]any)
	AuditSynchronous           func(*gin.Context, string, string, string, string, map[string]any)
	NotifyEvent                func(string, map[string]string)
	ReloadTokens               func()
	ComponentUIReadPermissions func(*gin.Context) []string
	IsBearer                   func(*gin.Context) bool
}

func ResetRateLimits() {
	securityVerificationAggregateRateLimit.ResetAll()
	securityVerificationMethodRateLimit.ResetAll()
}
