//go:build !minimal

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionhealth "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/health"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionobservation "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observation"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func (h Handler) diagnostics(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	inventory := protectionresources.Snapshot(c.Request.Context(), queryBool(c, "refresh"))
	support := domain.SupportSupported
	capabilityReason := h.firewallCapabilityReason(c.Request.Context())
	warnings := []string{}
	if capabilityReason != "" {
		warnings = append(warnings, capabilityReason)
	}
	if runtime.GOOS != "linux" {
		support = domain.SupportUnsupported
		warnings = append(warnings, "firewall preview is unsupported on this operating system")
	} else if len(inventory.Errors) > 0 || len(inventory.Collisions) > 0 {
		support = domain.SupportDegraded
	}
	observationStatus := protectionobservation.Status{}
	if h.deps.ObservationStatus != nil {
		observationStatus = h.deps.ObservationStatus()
	}
	health := protectionhealth.Evaluate(c.Request.Context(), inventory.Resources, nil)
	healthState := protectionhealth.Summary(health)
	if healthState == "missing_capability" || healthState == "degraded" {
		support = domain.SupportDegraded
	}
	h.deps.JSONObj(c, gin.H{
		"supportState": support,
		"checks": []gin.H{
			{"id": "resource-registry", "status": statusFrom(len(inventory.Errors) == 0), "details": len(inventory.Resources)},
			{"id": "listener-collisions", "status": statusFrom(len(inventory.Collisions) == 0), "details": len(inventory.Collisions)},
			{"id": "system-helper", "status": map[bool]string{true: "ok", false: "missing_capability"}[capabilityReason == ""], "details": map[bool]string{true: "restricted_helper_ready", false: capabilityReason}[capabilityReason == ""]},
			{"id": "observation-worker", "status": statusFrom(observationStatus.Running), "details": observationStatus},
		},
		"resourceHealth": health,
		"healthState":    healthState,
		"warnings":       warnings,
		"platform":       gin.H{"os": runtime.GOOS, "arch": runtime.GOARCH},
	}, nil)
}

type firewallPreviewInput struct {
	IncludeGeneratedNFT          bool   `json:"includeGeneratedNft"`
	IncludeGeneratedNFTSnake     bool   `json:"include_generated_nft"`
	ProfileIDs                   []uint `json:"profileIds"`
	ProfileIDsSnake              []uint `json:"profile_ids"`
	ExpectedBindingRevision      string `json:"expectedBindingRevision"`
	ExpectedBindingRevisionSnake string `json:"expected_binding_revision"`
}

func (h Handler) firewallPreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input firewallPreviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	profileIDs := append(append([]uint(nil), input.ProfileIDs...), input.ProfileIDsSnake...)
	plan, _, err := h.currentFirewallPlan(c, true)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	inventory := protectionresources.Snapshot(c.Request.Context(), false)
	if len(profileIDs) > 0 {
		selected := make(map[string]struct{}, len(profileIDs))
		for _, id := range profileIDs {
			profile, err := h.deps.Repository.Profile(c.Request.Context(), id)
			if err != nil {
				writeError(c, http.StatusBadRequest, "validation_error", fmt.Errorf("profile %d: %w", id, err))
				return
			}
			selected[profile.ResourceID] = struct{}{}
		}
		plan, _, err = h.currentEndpointFirewallPlan(c.Request.Context(), true, selected)
		if err != nil {
			h.deps.JSONObj(c, nil, err)
			return
		}
	}
	expectedBinding := input.ExpectedBindingRevision
	if expectedBinding == "" {
		expectedBinding = input.ExpectedBindingRevisionSnake
	}
	if expectedBinding != "" && (!exactAPIRevision(expectedBinding) || expectedBinding != plan.InputRevision) {
		writeError(c, http.StatusConflict, "revision_conflict", protectionfirewall.ErrPlanRevision)
		return
	}
	preview := protectionfirewall.Preview(plan, protectionfirewall.PreviewOptions{
		IncludeGeneratedNFT: input.IncludeGeneratedNFT || input.IncludeGeneratedNFTSnake,
		OperatingSystem:     runtime.GOOS,
	})
	preview.WouldWarn = append(preview.WouldWarn, collisionWarnings(inventory.Collisions)...)
	preview.WouldWarn = append(preview.WouldWarn, contributorWarnings(inventory.Errors)...)
	h.audit(c, "server_protection_firewall_preview", map[string]any{
		"revision": preview.Revision, "backend": preview.Backend, "resources": len(preview.ProtectedKeep),
		"open": len(preview.WouldOpen), "warnings": len(preview.WouldWarn),
	})
	h.deps.JSONObj(c, preview, nil)
}

func exactAPIRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (h Handler) currentFirewallPlan(c *gin.Context, refresh bool) (protectionfirewall.FirewallPlan, []hostresources.ProtectableResource, error) {
	return h.currentEndpointFirewallPlan(c.Request.Context(), refresh, nil)
}

func writeFirewallWorkflowError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, protectionoperations.ErrConfirmationRequired):
		writeError(c, http.StatusBadRequest, "confirmation_required", err)
	case errors.Is(err, protectionfirewall.ErrUnknownSSH):
		writeError(c, http.StatusConflict, "unknown_ssh", err)
	case errors.Is(err, protectionfirewall.ErrPlanRevision):
		writeError(c, http.StatusConflict, "revision_conflict", err)
	case errors.Is(err, protectionfirewall.ErrUnsafeResource):
		writeError(c, http.StatusConflict, "unsafe_resource_inventory", err)
	case errors.Is(err, protectionfirewall.ErrHelperRevision):
		writeError(c, http.StatusConflict, "helper_revision_conflict", err)
	case errors.Is(err, protectionfirewall.ErrHealthFailed):
		writeError(c, http.StatusConflict, "health_failed", err)
	case errors.Is(err, protectionfirewall.ErrMissingCapability):
		writeError(c, http.StatusConflict, "missing_capability", err)
	case errors.Is(err, protectionfirewall.ErrApplyVerify):
		writeError(c, http.StatusConflict, "apply_verify_mismatch", err)
	case errors.Is(err, protectionfirewall.ErrRollbackHealth):
		writeError(c, http.StatusConflict, "rollback_health_failed", err)
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "not_found", err)
	case errors.Is(err, protectionoperations.ErrFenced), errors.Is(err, protectionrepository.ErrOperationFenced):
		writeError(c, http.StatusConflict, "operation_fenced", err)
	default:
		writeError(c, http.StatusConflict, "rollback_failed", err)
	}
}

func (h Handler) readAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", "admin", readScope, writeScope, applyScope)
}

func (h Handler) writeAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", "admin", writeScope)
}

func (h Handler) applyAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", "admin", applyScope)
}

func (h Handler) applyEnabled(c *gin.Context) bool {
	if !h.applyAllowed(c) {
		return false
	}
	return h.applyConfigured(c)
}

func (h Handler) applyConfigured(c *gin.Context) bool {
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return false
	}
	if !settings.FeatureFlags["enable_apply_beta"] || settings.AdvancedAcknowledgedAt == 0 {
		writeError(c, http.StatusConflict, "missing_capability", errors.New("apply_beta_disabled"))
		return false
	}
	if reason := h.firewallCapabilityReason(c.Request.Context()); reason != "" {
		writeError(c, http.StatusConflict, "missing_capability", errors.New(reason))
		return false
	}
	inventory := protectionresources.Snapshot(c.Request.Context(), true)
	if err := firewallInventoryReady(inventory); err != nil {
		writeError(c, http.StatusConflict, "missing_capability", err)
		return false
	}
	healthState := protectionhealth.Summary(protectionhealth.Evaluate(c.Request.Context(), inventory.Resources, nil))
	if healthState != "ok" {
		writeError(c, http.StatusConflict, "missing_capability", errors.New("health_"+string(healthState)))
		return false
	}
	return true
}

func firewallInventoryReady(inventory protectionresources.InventorySnapshot) error {
	if len(inventory.Errors) > 0 || len(inventory.Resources) == 0 {
		return errors.New("protectable_resource_inventory_incomplete")
	}
	return nil
}

func (h Handler) firewallCapabilityReason(ctx context.Context) string {
	if h.deps.Firewall == nil {
		return "helper_not_installed"
	}
	capabilities, err := h.deps.Firewall.Capabilities(ctx)
	if err != nil || capabilities == nil {
		return "helper_capability_unknown"
	}
	if !capabilities.NFT.PlatformKnown {
		return "platform_capability_unknown"
	}
	if !capabilities.NFT.Linux {
		return "linux_required"
	}
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback} {
		if !protectionhelper.CapabilityAvailable(capabilities, operation) {
			if capabilities.NFT.Reason != "" {
				return capabilities.NFT.Reason
			}
			return "nft_capability_missing"
		}
	}
	return ""
}
