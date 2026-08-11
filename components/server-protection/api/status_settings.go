//go:build !minimal

package api

import (
	"context"
	"errors"
	"net/http"
	"runtime"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionhealth "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/health"
	protectionobservation "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observation"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func (h Handler) status(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	settings, revision, degradedSettings, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	inventory := protectionresources.Snapshot(c.Request.Context(), false)
	_, profileCount, profileErr := h.deps.Repository.ListProfiles(c.Request.Context(), protectionrepository.ProfileFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 1}})
	_, eventCount, eventErr := h.deps.Repository.ListEvents(c.Request.Context(), protectionrepository.EventFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 1}})
	if profileErr != nil || eventErr != nil {
		h.deps.JSONObj(c, nil, errors.Join(profileErr, eventErr))
		return
	}
	support := domain.SupportSupported
	if runtime.GOOS != "linux" || degradedSettings || len(inventory.Errors) > 0 || len(inventory.Collisions) > 0 {
		support = domain.SupportDegraded
	}
	observationStatus := protectionobservation.Status{}
	if h.deps.ObservationStatus != nil {
		observationStatus = h.deps.ObservationStatus()
	}
	recoveryRequired := 0
	if h.deps.Operations != nil {
		operations, operationErr := h.deps.Operations.List(c.Request.Context())
		if operationErr != nil {
			h.deps.JSONObj(c, nil, operationErr)
			return
		}
		for _, operation := range operations {
			if operation.State == protectionoperations.StateApplying || operation.State == protectionoperations.StateHealthFailed || operation.State == protectionoperations.StateRollingBack || operation.State == protectionoperations.StateRollbackFailed || operation.State == protectionoperations.StateLockSuspect {
				recoveryRequired++
			}
		}
	}
	health := protectionhealth.Evaluate(c.Request.Context(), inventory.Resources, nil)
	healthState := protectionhealth.Summary(health)
	if healthState == "missing_capability" || healthState == "degraded" {
		support = domain.SupportDegraded
	}
	capabilityReason := h.firewallCapabilityReason(c.Request.Context())
	if capabilityReason == "" && healthState != "ok" {
		capabilityReason = "health_" + string(healthState)
	}
	readiness := "apply_beta"
	blockers := []string{"public observation ingestion is not active until the host observation hook is connected"}
	if capabilityReason != "" {
		readiness = "preview_only"
		blockers = append([]string{capabilityReason}, blockers...)
	} else if !settings.FeatureFlags["enable_apply_beta"] || settings.AdvancedAcknowledgedAt == 0 {
		readiness = "apply_beta_disabled"
		blockers = append([]string{"apply_beta_disabled"}, blockers...)
	}
	h.deps.JSONObj(c, gin.H{
		"enabled":      settings.Enabled,
		"revision":     revision,
		"supportState": support,
		"readiness":    readiness,
		"blockers":     blockers,
		"counters": gin.H{
			"resources":  len(inventory.Resources),
			"collisions": len(inventory.Collisions),
			"profiles":   profileCount,
			"events":     eventCount,
			"recovery":   recoveryRequired,
		},
		"platform":       gin.H{"os": runtime.GOOS, "arch": runtime.GOARCH},
		"observations":   observationStatus,
		"resourceHealth": health,
		"healthState":    healthState,
		"fronting":       gin.H{"enabled": settings.FeatureFlags["enable_fronting_beta"], "capabilityReason": h.frontingCapabilityReason(c.Request.Context())},
	}, nil)
}

func (h Handler) settings(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	value, revision, degraded, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	h.deps.JSONObj(c, gin.H{"settings": value, "defaults": domain.DefaultSettings(), "revision": revision, "degraded": degraded, "readonlyFlags": []string{"enable_node_beta", "enable_hard_block"}}, err)
}

type settingsInput struct {
	Settings domain.Settings `json:"settings"`
	Revision int             `json:"revision"`
}

func (h Handler) updateSettings(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input settingsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	if input.Settings.FeatureFlags["enable_hard_block"] {
		writeError(c, http.StatusConflict, "missing_capability", errors.New("hard_block_primitive_unavailable"))
		return
	}
	for _, key := range []string{"enable_node_beta", "enable_external_integrations", "enable_desync_links"} {
		if input.Settings.FeatureFlags[key] {
			writeError(c, http.StatusConflict, "missing_capability", errors.New(key+"_unavailable"))
			return
		}
	}
	if input.Settings.FeatureFlags["enable_apply_beta"] {
		if reason := h.firewallCapabilityReason(c.Request.Context()); reason != "" {
			writeError(c, http.StatusConflict, "missing_capability", errors.New(reason))
			return
		}
	}
	if input.Settings.FeatureFlags["enable_fronting_beta"] {
		if reason := h.frontingCapabilityReason(c.Request.Context()); reason != "" {
			writeError(c, http.StatusConflict, "missing_capability", errors.New(reason))
			return
		}
	}
	if (input.Settings.FeatureFlags["enable_apply_beta"] || input.Settings.FeatureFlags["enable_fronting_beta"]) && input.Settings.AdvancedAcknowledgedAt == 0 {
		writeError(c, http.StatusBadRequest, "confirmation_required", errors.New("apply_beta_requires_advanced_acknowledgement"))
		return
	}
	revision, err := h.deps.Repository.SaveSettingsRevision(c.Request.Context(), input.Settings, input.Revision)
	if errors.Is(err, protectionrepository.ErrRevisionConflict) {
		writeError(c, http.StatusConflict, "revision_conflict", err)
		return
	}
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_settings_updated", map[string]any{"revision": revision})
	h.deps.JSONObj(c, gin.H{"settings": input.Settings, "revision": revision}, nil)
}

func (h Handler) frontingCapabilityReason(ctx context.Context) string {
	if h.deps.Fronting == nil {
		return "fronting_adapter_unavailable"
	}
	report := h.deps.Fronting.Capability(ctx)
	if !report.Supported {
		if report.Reason != "" {
			return report.Reason
		}
		return "fronting_helper_capability_unavailable"
	}
	return ""
}
