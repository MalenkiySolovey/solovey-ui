//go:build !minimal

package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func (h Handler) resources(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	refresh := queryBool(c, "refresh")
	inventory := protectionresources.Snapshot(c.Request.Context(), refresh)
	page := parsePage(c, 100, 500)
	kind := strings.TrimSpace(c.Query("kind"))
	owner := strings.TrimSpace(c.Query("owner"))
	filtered := inventory.Resources[:0]
	for _, item := range inventory.Resources {
		if kind != "" && item.Kind != kind {
			continue
		}
		if owner != "" && item.Owner != owner {
			continue
		}
		filtered = append(filtered, item)
	}
	inventory.Resources = filtered
	total := len(inventory.Resources)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.Limit
	if end > total {
		end = total
	}
	inventory.Resources = inventory.Resources[start:end]
	h.deps.JSONObj(c, gin.H{"generatedAt": inventory.GeneratedAt, "resources": inventory.Resources, "items": inventory.Resources, "collisions": inventory.Collisions, "warnings": inventory.Warnings, "errors": inventory.Errors, "page": page.Page, "limit": page.Limit, "total": total}, nil)
}

type profileInput struct {
	ResourceID       string                `json:"resourceId"`
	ResourceRevision string                `json:"resourceRevision"`
	Mode             domain.ProfileMode    `json:"mode"`
	Enabled          bool                  `json:"enabled"`
	ScoreThreshold   int                   `json:"scoreThreshold"`
	GraylistTTL      int                   `json:"graylistTtlSeconds"`
	DefaultAction    domain.DecisionAction `json:"defaultAction"`
	Revision         int                   `json:"revision"`
}

type profileView struct {
	ID                  uint                  `json:"id"`
	ResourceID          string                `json:"resourceId"`
	ResourceKind        string                `json:"resourceKind"`
	ResourceOwner       string                `json:"resourceOwner"`
	InboundTag          string                `json:"inboundTag,omitempty"`
	Enabled             bool                  `json:"enabled"`
	Status              string                `json:"status"`
	Mode                domain.ProfileMode    `json:"mode"`
	ResourceFingerprint string                `json:"resourceFingerprint"`
	AcceptedFingerprint string                `json:"acceptedFingerprint"`
	LastSeenFingerprint string                `json:"lastSeenFingerprint"`
	ScoreThreshold      int                   `json:"scoreThreshold"`
	GraylistTTL         int                   `json:"graylistTtlSeconds"`
	DefaultAction       domain.DecisionAction `json:"defaultAction"`
	Revision            int                   `json:"revision"`
	CreatedAt           int64                 `json:"createdAt"`
	UpdatedAt           int64                 `json:"updatedAt"`
}

func (h Handler) profiles(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 50, 200)
	items, total, err := h.deps.Repository.ListProfiles(c.Request.Context(), protectionrepository.ProfileFilter{PageQuery: page, ResourceID: strings.TrimSpace(c.Query("resource_id")), Status: strings.TrimSpace(c.Query("status"))})
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	current := resourceMap(protectionresources.Snapshot(c.Request.Context(), false).Resources)
	views := make([]profileView, 0, len(items))
	for _, item := range items {
		views = append(views, makeProfileView(item, current[item.ResourceID]))
	}
	h.deps.JSONObj(c, gin.H{"items": views, "page": page.Page, "limit": page.Limit, "total": total}, nil)
}

func (h Handler) createProfile(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input profileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	resource, err := resolveResource(c, input.ResourceID, input.ResourceRevision)
	if err != nil {
		writeProfileError(c, err)
		return
	}
	settings, _, err := h.deps.Repository.LoadSettings(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	normalizeProfileInput(&input, settings)
	if err := validateMVPProfile(input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	now := time.Now().Unix()
	item := protectionrepository.ProfileModel{
		ResourceID: resource.ID, ResourceKind: resource.Kind, ResourceOwner: resource.Owner,
		InboundTag: resource.InboundTag, Enabled: input.Enabled, Status: profileStatus(input.Enabled), Mode: string(input.Mode),
		ResourceFingerprint: resource.Fingerprint, AcceptedFingerprint: resource.Fingerprint, LastSeenFingerprint: resource.Fingerprint,
		PublicListen: resource.Listen, PublicPort: optionalPort(resource.Port), ScoreThreshold: input.ScoreThreshold,
		GraylistTTLSeconds: input.GraylistTTL, DefaultAction: string(input.DefaultAction), ManagedFirewall: false,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.deps.Repository.CreateProfile(c.Request.Context(), &item); err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_profile_created", map[string]any{"profileId": item.ID, "resourceId": item.ResourceID, "mode": item.Mode})
	h.deps.JSONObj(c, makeProfileView(item, resource), nil)
}

func (h Handler) updateProfile(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input profileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	item, err := h.deps.Repository.Profile(c.Request.Context(), id)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	settings, _, err := h.deps.Repository.LoadSettings(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	normalizeProfileInput(&input, settings)
	if err := validateMVPProfile(input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	item.Enabled = input.Enabled
	item.Status = profileStatus(input.Enabled)
	item.Mode = string(input.Mode)
	item.ScoreThreshold = input.ScoreThreshold
	item.GraylistTTLSeconds = input.GraylistTTL
	item.DefaultAction = string(input.DefaultAction)
	item.ManagedFirewall = false
	item.UpdatedAt = time.Now().Unix()
	if err := h.deps.Repository.UpdateProfile(c.Request.Context(), &item, input.Revision); err != nil {
		if errors.Is(err, protectionrepository.ErrRevisionConflict) {
			writeError(c, http.StatusConflict, "revision_conflict", err)
			return
		}
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_profile_updated", map[string]any{"profileId": item.ID, "revision": item.Revision})
	resource := resourceMap(protectionresources.Snapshot(c.Request.Context(), false).Resources)[item.ResourceID]
	h.deps.JSONObj(c, makeProfileView(item, resource), nil)
}

func (h Handler) deleteProfile(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	err := h.deps.Repository.DeleteProfile(c.Request.Context(), id)
	if err == nil {
		h.audit(c, "server_protection_profile_deleted", map[string]any{"profileId": id})
	}
	h.deps.JSONMsg(c, "deleted", err)
}

type reattachInput struct {
	ResourceID string `json:"resourceId"`
	Revision   int    `json:"revision"`
}

func (h Handler) reattachProfile(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input reattachInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	resource, err := resolveResource(c, input.ResourceID, "")
	if err != nil {
		writeProfileError(c, err)
		return
	}
	item, err := h.deps.Repository.Profile(c.Request.Context(), id)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	item.ResourceID, item.ResourceKind, item.ResourceOwner = resource.ID, resource.Kind, resource.Owner
	item.InboundTag = resource.InboundTag
	item.ResourceFingerprint, item.AcceptedFingerprint, item.LastSeenFingerprint = resource.Fingerprint, resource.Fingerprint, resource.Fingerprint
	item.PublicListen, item.PublicPort = resource.Listen, optionalPort(resource.Port)
	item.Status, item.UpdatedAt = profileStatus(item.Enabled), time.Now().Unix()
	if err := h.deps.Repository.UpdateProfile(c.Request.Context(), &item, input.Revision); err != nil {
		if errors.Is(err, protectionrepository.ErrRevisionConflict) {
			writeError(c, http.StatusConflict, "revision_conflict", err)
			return
		}
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_profile_reattached", map[string]any{"profileId": id, "resourceId": resource.ID})
	h.deps.JSONObj(c, makeProfileView(item, resource), nil)
}
