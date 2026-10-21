package api

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

// actionPolicy adapts the existing authenticated request/scope and one-use
// step-up boundaries. The owner invokes it with its actual typed input.
func (a *ApiService) actionPolicy(requireStepUp func(*gin.Context, string, string) bool) func(*gin.Context) actions.Authorizer {
	return func(c *gin.Context) actions.Authorizer {
		return func(ctx context.Context, invocation actions.Invocation) (actions.Decision, error) {
			deny := func() (actions.Decision, error) {
				if c != nil && c.Request != nil && !c.IsAborted() {
					id := "unknown"
					if descriptor, known := actions.Lookup(invocation.ID); known {
						id = descriptor.ID
					}
					a.recordAudit(c, requestActor(c), "semantic_action_denied", "security", service.AuditSeverityWarn, map[string]any{"actionId": id})
					c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "semantic action denied"})
				}
				return actions.Decision{}, actions.ErrDenied
			}
			if c == nil || c.Request == nil || ctx == nil || ctx.Err() != nil || invocation.Validate() != nil || requireStepUp == nil {
				return deny()
			}
			descriptor, known := actions.Lookup(invocation.ID)
			if !known || !actions.KnownPermission(descriptor.Permission) {
				return deny()
			}
			actualComponent := c.GetString(componentAuthorityKey)
			if descriptor.ComponentID != "" {
				component, registered := registry.ComponentByID(actualComponent)
				if !registered || !actions.ComponentAllowed(actualComponent, component.Manifest.Actions, descriptor.ID) {
					return deny()
				}
			} else if actualComponent != "" {
				return deny()
			}
			permitted := false
			switch descriptor.Permission {
			case actions.PermissionBackupRestore:
				permitted = a.requireTokenScopeAny(c, "database", "admin")
			case actions.PermissionComponentUninstall:
				permitted = a.requireTokenScopeAny(c, "update", "admin", "update")
			default:
				return deny()
			}
			if !permitted {
				c.Abort()
				return actions.Decision{}, actions.ErrDenied
			}
			if !requireStepUp(c, descriptor.ID, invocation.Target) {
				c.Abort()
				return actions.Decision{}, actions.ErrDenied
			}
			actor := requestActor(c)
			if actor == "" {
				return deny()
			}
			return actions.Decision{Allowed: true, Actor: actor, Permission: descriptor.Permission, Invocation: invocation}, nil
		}
	}
}
