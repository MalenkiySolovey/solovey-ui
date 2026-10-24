package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/service"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
)

type providerPreviewRequest struct {
	Channel        release.Channel `json:"channel"`
	TargetProvider string          `json:"targetProvider"`
	IdempotencyKey string          `json:"idempotencyKey"`
}

type providerMaintainRequest struct {
	updateservice.ProviderTransitionRequest
	Confirmation string `json:"confirmation"`
}

// These routes are the authenticated transport used by the administrative CLI.
// They project and invoke Update, never select a provider through Settings.
func (h *updateHTTP) providerPreview(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok || !h.api.requireTokenScopeAny(c, "update", "admin", "update") {
		return
	}
	var request providerPreviewRequest
	if !decodeSecurityJSON(c, &request) || !validUpdateChannel(request.Channel) ||
		(request.TargetProvider != "sing-box" && request.TargetProvider != "shtorm-extended") || !safeDeploymentID(request.IdempotencyKey, 96) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid provider maintenance preview")
		}
		return
	}
	preview, err := h.manager.PreviewProviderTransition(c.Request.Context(), request.Channel, request.TargetProvider)
	if err != nil {
		h.writeError(c, "provider_maintenance_preview", err, preview)
		return
	}
	apply := updateservice.ProviderTransitionRequest{Channel: request.Channel, TargetProvider: request.TargetProvider,
		ExpectedSequence: preview.Sequence, ExpectedManifestDigest: preview.ManifestDigest, ExpectedArtifactSetDigest: preview.ArtifactSetDigest,
		ExpectedConfigurationRevision: preview.Configuration.ConfigurationRevision, ExpectedProviderRevision: preview.Current.Revision,
		IdempotencyKey: request.IdempotencyKey, Acknowledged: true}
	invocation := updateservice.ProviderTransitionAction(apply)
	jsonObj(c, gin.H{"preview": preview, "request": apply, "confirmation": updateservice.ProviderTransitionConfirmation(apply),
		"actionId": invocation.ID, "stepUpTargetDigest": securityTargetDigest(invocation.Target)}, nil)
}

func (h *updateHTTP) maintainProvider(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request providerMaintainRequest
	if !decodeSecurityJSON(c, &request) || request.ProviderTransitionRequest.Validate() != nil ||
		request.Confirmation != updateservice.ProviderTransitionConfirmation(request.ProviderTransitionRequest) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid provider maintenance request")
		}
		return
	}
	// Update reconstructs the invocation from its typed request and invokes
	// this policy before journal writes, downloads, checkpointing or Core hold.
	policy := h.api.actionPolicy(h.api.requireStepUpAction)(c)
	request.Authorize = func(ctx context.Context, invocation actions.Invocation) (actions.Decision, error) {
		decision, err := policy(ctx, invocation)
		if err == nil && decision.Allowed {
			// Record acceptance before the owner retains SQLite maintenance.
			// The original HTTP context cannot write through that later hold.
			h.api.recordAuditSynchronous(c, securityContext.Username, "core_provider_maintenance_requested", "update", service.AuditSeverityWarn,
				map[string]any{"operationId": invocation.Reference.OperationID, "sequence": request.ExpectedSequence,
					"targetProvider": request.TargetProvider, "planDigest": invocation.InputDigest, "correlation": invocation.Reference.Correlation()})
		}
		return decision, err
	}
	operation, err := h.manager.ApplyProviderTransition(c.Request.Context(), request.ProviderTransitionRequest)
	if c.IsAborted() {
		return
	}
	if errors.Is(err, actions.ErrDenied) {
		c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "semantic action denied"})
		return
	}
	if err != nil {
		h.writeError(c, "provider_maintenance", err, updateservice.ProjectOperation(operation))
		return
	}
	jsonObj(c, updateservice.ProjectOperation(operation), nil)
}
