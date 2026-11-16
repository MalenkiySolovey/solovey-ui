// Package componentroutes defines the Component inputs supplied to Backend HTTP composition.
package componentroutes

import (
	"context"
	"errors"
	"time"

	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/backend/actions"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentview"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/gin-gonic/gin"
)

var ErrUnavailable = errors.New("component composition unavailable")

type AuditRecorder = hostports.AuditRecorder

// Ports contains request authority and presentation helpers. The Product binds
// implementation owners before registering its selected Component routes.
type ComponentSettings interface {
	SetComponentEnabled(id string, enabled bool, actor, hostname string) error
}

type Ports struct {
	ComponentSettings ComponentSettings
	Auth              AuthDeps
	Request           RequestDeps
	Rate              RateLimitDeps
	Audit             AuditDeps
	HTTP              HTTPDeps
	Update            UpdateDeps
}

type AuthDeps struct {
	// AuthorizationChanged refreshes the host's authorization snapshot after
	// a feature has committed a change to account or token authority.
	ActionPolicy           func(*gin.Context) actions.Authorizer
	AuthorizationChanged   func()
	RequireScope           func(*gin.Context, string, ...string) bool
	RequireAuditAdminScope func(*gin.Context) bool
	RequireStepUp          func(*gin.Context, string, string) bool
	LoginUser              func(*gin.Context) string
	CheckPassword          func(string, string, string) bool
}

type RequestDeps struct {
	Actor          func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	ClientIdentity func(*gin.Context) clientidentity.V1
	Hostname       func(*gin.Context) string
	ValidateTarget func(context.Context, string) error
}

type RateLimitDeps struct {
	CheckRateLimit        func(string) (time.Duration, error)
	CheckLoginRateLimit   func(string) error
	RecordLoginFailure    func(string)
	ResetLoginFailures    func(string)
	LoginRateLimitUserKey func(string) string
	CheckAuditRateLimit   func(string) error
	AuditRateLimitKey     func(string, string) string
	AuditRateLimitWindow  time.Duration
}

type AuditDeps struct {
	Audit    func(*gin.Context, string, string, string, string, map[string]any)
	Recorder AuditRecorder
}

type HTTPDeps struct {
	JSONObj func(*gin.Context, interface{}, error)
	JSONMsg func(*gin.Context, string, error)
}

type UpdateDeps struct {
	AllowForcedUpdateCheck func() bool
}

// Route is one registration supplied by the existing installed Component owner.
// Backend wraps it in its authenticated and active-component middleware.
type Route struct {
	ComponentID string
	Register    func(*gin.RouterGroup) error
}

// OwnerAvailability is the installed owner's existing availability and enabled
// observation. It does not grant lifecycle, storage or execution authority.
type OwnerAvailability struct {
	ID                 string
	Available          bool
	Enabled            bool
	EnabledUnavailable bool
}

// Composition is the Product's selected Component read and registration input.
// It contains no registry, lifecycle engine, database or broad runtime object.
type Composition interface {
	Routes(Ports) ([]Route, error)
	Inventory() (componentview.RuntimeInventory, error)
	IsActive(string) (bool, error)
	DeclaresAction(componentID, actionID string) bool
	Owners() ([]OwnerAvailability, error)
}

// Inventory rejects missing composition before any Panel data is published.
func Inventory(owner Composition) (componentview.RuntimeInventory, error) {
	if owner == nil {
		return componentview.RuntimeInventory{}, ErrUnavailable
	}
	return owner.Inventory()
}
