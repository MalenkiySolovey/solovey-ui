//go:build !minimal

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func normalizeProfileInput(input *profileInput, settings domain.Settings) {
	if input.Mode == "" {
		input.Mode = domain.ProfileModeRecordOnly
	}
	if input.ScoreThreshold == 0 {
		input.ScoreThreshold = settings.DefaultScoreThreshold
	}
	if input.GraylistTTL == 0 {
		input.GraylistTTL = settings.DefaultGraylistTTLSeconds
	}
	if input.DefaultAction == "" {
		input.DefaultAction = domain.DecisionRecordOnly
	}
}

func validateMVPProfile(input profileInput) error {
	if input.Mode != domain.ProfileModeRecordOnly && input.Mode != domain.ProfileModeMetadataOnly {
		return errors.New("preview-only MVP supports record_only and metadata_only profiles")
	}
	if input.DefaultAction != domain.DecisionRecordOnly {
		return errors.New("preview-only MVP supports only the record_only decision")
	}
	if input.ScoreThreshold < 1 || input.ScoreThreshold > domain.DefaultMaxScore {
		return fmt.Errorf("scoreThreshold must be between 1 and %d", domain.DefaultMaxScore)
	}
	if input.GraylistTTL < 60 || input.GraylistTTL > 604800 {
		return errors.New("graylistTtlSeconds must be between 60 and 604800")
	}
	return nil
}

func resolveResource(c *gin.Context, resourceID, revision string) (hostresources.ProtectableResource, error) {
	resourceID = strings.TrimSpace(resourceID)
	for _, item := range protectionresources.Snapshot(c.Request.Context(), false).Resources {
		if item.ID != resourceID {
			continue
		}
		if revision != "" && revision != item.Fingerprint && revision != item.Capabilities.ConfigRevision {
			return hostresources.ProtectableResource{}, protectionrepository.ErrRevisionConflict
		}
		if err := domain.ResourceKind(item.Kind).Validate(); err != nil {
			return hostresources.ProtectableResource{}, fmt.Errorf("unsupported resource kind: %w", err)
		}
		return item, nil
	}
	return hostresources.ProtectableResource{}, protectionrepository.ErrRecordNotFound
}

func writeProfileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "resource_disappeared", err)
	case errors.Is(err, protectionrepository.ErrRevisionConflict):
		writeError(c, http.StatusConflict, "revision_conflict", err)
	default:
		writeError(c, http.StatusBadRequest, "unsupported", err)
	}
}

func makeProfileView(item protectionrepository.ProfileModel, resource hostresources.ProtectableResource) profileView {
	status := item.Status
	lastSeen := item.LastSeenFingerprint
	if resource.ID == "" {
		status = "stale"
	} else {
		lastSeen = resource.Fingerprint
		if item.AcceptedFingerprint != resource.Fingerprint {
			status = "stale"
		}
	}
	return profileView{
		ID: item.ID, ResourceID: item.ResourceID, ResourceKind: item.ResourceKind, ResourceOwner: item.ResourceOwner,
		InboundTag: item.InboundTag, Enabled: item.Enabled, Status: status, Mode: domain.ProfileMode(item.Mode),
		ResourceFingerprint: item.ResourceFingerprint, AcceptedFingerprint: item.AcceptedFingerprint, LastSeenFingerprint: lastSeen,
		ScoreThreshold: item.ScoreThreshold, GraylistTTL: item.GraylistTTLSeconds, DefaultAction: domain.DecisionAction(item.DefaultAction),
		Revision: item.Revision, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func resourceMap(items []hostresources.ProtectableResource) map[string]hostresources.ProtectableResource {
	result := make(map[string]hostresources.ProtectableResource, len(items))
	for _, item := range items {
		result[item.ID] = item
	}
	return result
}

func profileStatus(enabled bool) string {
	if enabled {
		return "active"
	}
	return "disabled"
}
