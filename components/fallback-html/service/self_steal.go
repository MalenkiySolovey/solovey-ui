//go:build !minimal

package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	inbounddrafts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/drafts"
	"gorm.io/gorm"
)

type SelfStealDraftInput struct {
	Profile       string `json:"profile"`
	TargetID      uint   `json:"targetId"`
	HandshakeHost string `json:"handshakeHost"`
}

type SelfStealDraftView struct {
	ID          uint                  `json:"id"`
	SiteID      uint                  `json:"siteId"`
	CoreDraftID uint                  `json:"coreDraftId"`
	Status      string                `json:"status"`
	Payload     SelfStealDraftPayload `json:"payload"`
	CreatedAt   int64                 `json:"createdAt"`
}

type SelfStealDraftPayload struct {
	Schema               string     `json:"schema"`
	Source               string     `json:"source"`
	Profile              string     `json:"profile"`
	NoApply              bool       `json:"noApply"`
	RequiresCapability   string     `json:"requiresCapability"`
	CoreDraftID          uint       `json:"coreDraftId,omitempty"`
	SiteName             string     `json:"siteName"`
	Target               TargetView `json:"target"`
	ActivePublish        string     `json:"activePublish"`
	HandshakeHost        string     `json:"handshakeHost"`
	InboundType          string     `json:"inboundType"`
	InboundTag           string     `json:"inboundTag"`
	InboundCandidate     any        `json:"inboundCandidate"`
	Warnings             []string   `json:"warnings"`
	Blocks               []string   `json:"blocks"`
	ConservativeDefaults []string   `json:"conservativeDefaults"`
	NextSteps            []string   `json:"nextSteps"`
}

func (s *Service) CreateSelfStealDraft(siteID uint, input SelfStealDraftInput, actor string) (SelfStealDraftView, error) {
	site, err := s.GetSite(siteID)
	if err != nil {
		return SelfStealDraftView{}, err
	}
	profile, err := normalizeSelfStealProfile(input.Profile)
	if err != nil {
		return SelfStealDraftView{}, err
	}
	targets, err := s.targetsForSite(site)
	if err != nil {
		return SelfStealDraftView{}, err
	}
	target, err := selectSelfStealTarget(targets, input.TargetID)
	if err != nil {
		return SelfStealDraftView{}, err
	}
	report, err := s.safetyForSite(site)
	if err != nil {
		return SelfStealDraftView{}, err
	}

	var publish fallbackdomain.Publish
	activePublish := ""
	err = s.db.
		Where("site_id = ? AND active = ?", siteID, true).
		Order("id DESC").
		First(&publish).Error
	if err == nil {
		activePublish = publish.Version
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return SelfStealDraftView{}, err
	}

	warnings := append([]string(nil), report.Warnings...)
	blocks := []string{}
	if activePublish == "" {
		blocks = append(blocks, "publish the site before creating a self-steal inbound draft")
	}
	if !report.OK {
		blocks = append(blocks, "site safety report is not green")
	}
	if target.Status != "available" {
		blocks = append(blocks, "selected runtime target is not available: "+target.Reason)
	}
	if !target.TLS {
		blocks = append(blocks, "self-steal requires a TLS-capable public site target")
	}
	if !site.Enabled {
		blocks = append(blocks, "site is disabled")
	}
	handshakeHost := normalizeSelfStealHandshakeHost(input.HandshakeHost, target)
	inboundType := "vless"
	inboundTag := fmt.Sprintf("fallback-html-site-%d", site.ID)
	candidate := selfStealInboundCandidate(inboundTag, target, handshakeHost)

	payload := SelfStealDraftPayload{
		Schema:             "solovey-ui/fallback-html-self-steal-draft/v1",
		Source:             "fallback-html:self-steal",
		Profile:            profile,
		NoApply:            true,
		RequiresCapability: "inbound-draft",
		SiteName:           site.Name,
		Target:             target,
		ActivePublish:      activePublish,
		HandshakeHost:      handshakeHost,
		InboundType:        inboundType,
		InboundTag:         inboundTag,
		InboundCandidate:   candidate,
		Warnings:           warnings,
		Blocks:             blocks,
		ConservativeDefaults: []string{
			"sing-box REALITY server draft only",
			"no DPI-triggering XHTTP behavioral overrides",
			"no hidden Xray-core dependency",
		},
		NextSteps: []string{
			"review the core-owned inbound draft from the Inbounds editor",
			"apply it only after confirming clients, TLS/REALITY material, and listen ownership",
		},
	}
	status := "blocked"
	if len(blocks) == 0 {
		status = "ready"
	}
	now := time.Now().Unix()
	var draft fallbackdomain.SelfStealDraft
	var coreDraftID uint
	err = s.db.Transaction(func(tx *gorm.DB) error {
		expiredBefore := now - int64(selfStealDraftTTL/time.Second)
		if err := tx.Where("created_at < ?", expiredBefore).Delete(&fallbackdomain.SelfStealDraft{}).Error; err != nil {
			return err
		}
		if err := inbounddrafts.CleanupExpired(tx, now); err != nil {
			return err
		}
		coreStatus := inbounddrafts.StatusBlocked
		if status == "ready" {
			coreStatus = inbounddrafts.StatusReviewRequired
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		coreDraft, err := inbounddrafts.Create(tx, inbounddrafts.CreateInput{
			Source:      "fallback-html:self-steal",
			SourceRef:   fmt.Sprintf("site/%d/publish/%s/target/%s", siteID, activePublish, target.Runtime),
			Status:      coreStatus,
			InboundType: inboundType,
			Tag:         inboundTag,
			Payload:     raw,
			CreatedBy:   actor,
			ExpiresAt:   now + int64(selfStealDraftTTL/time.Second),
			Now:         now,
		})
		if err != nil {
			return err
		}
		coreDraftID = coreDraft.Id
		payload.CoreDraftID = coreDraftID
		raw, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Model(&coreDraft).Update("payload", raw).Error; err != nil {
			return err
		}
		draft = fallbackdomain.SelfStealDraft{
			SiteID:      siteID,
			CoreDraftID: coreDraftID,
			Status:      status,
			Payload:     raw,
			CreatedAt:   now,
		}
		if err := tx.Create(&draft).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "self_steal_draft_"+status, map[string]any{
			"profile": profile,
			"target":  target.Runtime,
			"blocks":  len(blocks),
		})
	})
	if err != nil {
		return SelfStealDraftView{}, err
	}
	return SelfStealDraftView{ID: draft.ID, SiteID: siteID, CoreDraftID: coreDraftID, Status: status, Payload: payload, CreatedAt: now}, nil
}

func selfStealInboundCandidate(tag string, target TargetView, handshakeHost string) map[string]any {
	listen := target.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	return map[string]any{
		"type":        "vless",
		"tag":         tag,
		"listen":      listen,
		"listen_port": target.Port,
		"users":       []any{},
		"tls": map[string]any{
			"enabled":     true,
			"server_name": handshakeHost,
			"reality": map[string]any{
				"enabled": true,
			},
		},
		"transport": map[string]any{
			"type": "tcp",
		},
	}
}
