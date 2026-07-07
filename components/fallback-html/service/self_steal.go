//go:build !minimal

package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	inbounddrafts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/drafts"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/gorm"
)

const (
	selfStealPublicPort       = 443
	selfStealDefaultLocalPort = 8443
	selfStealDefaultLocalHost = "127.0.0.1"
	selfStealDefaultTransport = "tcp"
	selfStealProfileVLESS     = "vless-reality"
	selfStealProfileTrojan    = "trojan-tls-fallback"
)

type SelfStealDraftInput struct {
	Profile         string `json:"profile"`
	Transport       string `json:"transport"`
	TargetID        uint   `json:"targetId"`
	HandshakeHost   string `json:"handshakeHost"`
	PublicListen    string `json:"publicListen"`
	PrepareTransfer bool   `json:"prepareTransfer"`
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
	Schema               string                 `json:"schema"`
	Source               string                 `json:"source"`
	Profile              string                 `json:"profile"`
	NoApply              bool                   `json:"noApply"`
	RequiresCapability   string                 `json:"requiresCapability"`
	CoreDraftID          uint                   `json:"coreDraftId,omitempty"`
	SiteName             string                 `json:"siteName"`
	Target               TargetView             `json:"target"`
	ActivePublish        string                 `json:"activePublish"`
	HandshakeHost        string                 `json:"handshakeHost"`
	PublicListen         string                 `json:"publicListen"`
	PublicPort           int                    `json:"publicPort"`
	Transport            string                 `json:"transport"`
	HandshakeTarget      TargetView             `json:"handshakeTarget"`
	PortTransfer         *PortOwnershipTransfer `json:"portTransfer,omitempty"`
	TLSRecordID          uint                   `json:"tlsRecordId,omitempty"`
	RealityPublicKey     string                 `json:"realityPublicKey,omitempty"`
	RealityShortID       string                 `json:"realityShortId,omitempty"`
	InboundType          string                 `json:"inboundType"`
	InboundTag           string                 `json:"inboundTag"`
	InboundCandidate     any                    `json:"inboundCandidate"`
	Warnings             []string               `json:"warnings"`
	Blocks               []string               `json:"blocks"`
	ConservativeDefaults []string               `json:"conservativeDefaults"`
	NextSteps            []string               `json:"nextSteps"`
}

type PortOwnershipTransfer struct {
	Required       bool       `json:"required"`
	Prepared       bool       `json:"prepared"`
	Reason         string     `json:"reason"`
	PreviousTarget TargetView `json:"previousTarget"`
	NewTarget      TargetView `json:"newTarget"`
}

type selfStealProfileSpec struct {
	ID          string
	InboundType string
	TLSMode     string
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
	profileSpec := selfStealProfileSpecFor(profile)
	transport, transportConfig, err := normalizeSelfStealTransport(input.Transport, site, input.HandshakeHost)
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
	blocks := []string{}
	publicListen, publicListenWarnings, publicListenBlocks := resolveSelfStealPublicListen(input.PublicListen, site, target)
	blocks = append(blocks, publicListenBlocks...)
	targetForDraft := target
	transfer := planSelfStealPortTransfer(target)
	transferWarnings := []string{}
	if transfer.Required {
		if input.PrepareTransfer {
			targetForDraft = transfer.NewTarget
			transfer.Prepared = true
			transferWarnings = append(transferWarnings, "port 443 ownership will move to the reviewed inbound; fallback-html will keep the decoy site on a local TLS target")
		} else {
			blocks = append(blocks, "port ownership transfer is required: move the fallback site to a local TLS target before the inbound can own public 443")
		}
	}
	siteForSafety := site
	if transfer.Required && transfer.Prepared {
		siteForSafety.Targets = []fallbackdomain.RuntimeTarget{targetFromView(site.ID, targetForDraft, time.Now().Unix())}
	}
	report, err := s.safetyForSite(siteForSafety)
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
	warnings = append(warnings, publicListenWarnings...)
	warnings = append(warnings, transferWarnings...)
	if activePublish == "" {
		blocks = append(blocks, "publish the site before creating a self-steal inbound draft")
	}
	if !report.OK {
		blocks = append(blocks, "site safety report is not green")
	}
	if !selfStealTargetUsable(targetForDraft) {
		blocks = append(blocks, "selected runtime target is not available: "+targetForDraft.Reason)
	}
	if !targetForDraft.TLS {
		blocks = append(blocks, "self-steal requires a TLS-capable public site target")
	}
	if !site.Enabled {
		blocks = append(blocks, "site is disabled")
	}
	if targetForDraft.TLS {
		certFile, keyFile := runtimeTLSFiles()
		if certFile == "" || keyFile == "" {
			blocks = append(blocks, "TLS fallback target requires panel certificate and key settings")
		}
	}
	if isWildcardListen(targetForDraft.Listen) {
		warnings = append(warnings, "fallback site target uses a wildcard listen address; prefer a local TLS target such as 127.0.0.1:8443")
	}
	handshakeHost := normalizeSelfStealHandshakeHost(input.HandshakeHost, targetForDraft)
	inboundType := profileSpec.InboundType
	inboundTag := fmt.Sprintf("fallback-html-site-%d", site.ID)
	candidate := selfStealInboundCandidate(profileSpec, inboundTag, publicListen, 0, targetForDraft, transportConfig)

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
		PublicListen:       publicListen,
		PublicPort:         selfStealPublicPort,
		Transport:          transport,
		HandshakeTarget:    targetForDraft,
		InboundType:        inboundType,
		InboundTag:         inboundTag,
		InboundCandidate:   candidate,
		Warnings:           warnings,
		Blocks:             blocks,
		ConservativeDefaults: []string{
			"core-owned inbound draft only; fallback-html never applies the inbound directly",
			"public 443 is owned by sing-box only after review/save in the Inbounds editor",
			"fallback site stays on a separate TLS target and is used as REALITY handshake or Trojan fallback",
			"no DPI-triggering XHTTP behavioral overrides in fallback-html",
			"stream graylist and firewall changes belong to inbound-protection",
		},
		NextSteps: []string{
			"keep the fallback site published on the selected TLS target",
			"review the core-owned inbound draft from the Inbounds editor",
			"apply it only after confirming clients and that port 443 can be owned by sing-box",
		},
	}
	if transfer.Required {
		payload.PortTransfer = &transfer
		if transfer.Prepared {
			payload.NextSteps = append([]string{"fallback-html target was prepared for local TLS handoff"}, payload.NextSteps...)
		}
	}
	status := "blocked"
	if len(blocks) == 0 {
		status = "ready"
	}
	var realityPrivateKey, realityPublicKey, realityShortID string
	if status == "ready" && profileSpec.ID == selfStealProfileVLESS {
		realityPrivateKey, realityPublicKey, err = generateSelfStealRealityKeyPair()
		if err != nil {
			return SelfStealDraftView{}, err
		}
		realityShortID, err = generateSelfStealShortID()
		if err != nil {
			return SelfStealDraftView{}, err
		}
	}
	now := time.Now().Unix()
	var draft fallbackdomain.SelfStealDraft
	var coreDraftID uint
	var tlsRecordID uint
	transferSaved := false
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
			if transfer.Required && transfer.Prepared {
				savedTarget, err := saveSelfStealTransferTarget(tx, siteID, targetForDraft, now)
				if err != nil {
					return err
				}
				transferSaved = true
				targetForDraft = savedTarget
				transfer.NewTarget = savedTarget
				payload.Target = target
				payload.HandshakeTarget = targetForDraft
				payload.PortTransfer = &transfer
			}
			tlsRecord, err := createSelfStealTLSRecord(tx, profileSpec, site.Name, handshakeHost, targetForDraft, realityPrivateKey, realityPublicKey, realityShortID)
			if err != nil {
				return err
			}
			tlsRecordID = tlsRecord.Id
			payload.TLSRecordID = tlsRecordID
			if profileSpec.ID == selfStealProfileVLESS {
				payload.RealityPublicKey = realityPublicKey
				payload.RealityShortID = realityShortID
			}
			payload.InboundCandidate = selfStealInboundCandidate(profileSpec, inboundTag, publicListen, tlsRecordID, targetForDraft, transportConfig)
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
			"profile":          profile,
			"transport":        transport,
			"target":           targetForDraft.Runtime,
			"blocks":           len(blocks),
			"transferPrepared": transfer.Prepared,
		})
	})
	if err != nil {
		return SelfStealDraftView{}, err
	}
	if transferSaved {
		if err := s.runtime.Rebuild(s.db); err != nil {
			return SelfStealDraftView{}, err
		}
	}
	return SelfStealDraftView{ID: draft.ID, SiteID: siteID, CoreDraftID: coreDraftID, Status: status, Payload: payload, CreatedAt: now}, nil
}

func selfStealInboundCandidate(profile selfStealProfileSpec, tag string, publicListen string, tlsID uint, target TargetView, transport map[string]any) map[string]any {
	candidate := map[string]any{
		"type":        profile.InboundType,
		"tag":         tag,
		"listen":      publicListen,
		"listen_port": selfStealPublicPort,
		"tls_id":      tlsID,
	}
	if len(transport) > 0 {
		candidate["transport"] = transport
	}
	if profile.ID == selfStealProfileTrojan {
		fallback := map[string]any{
			"server":      selfStealHandshakeServer(target),
			"server_port": target.Port,
		}
		candidate["fallback"] = fallback
		candidate["fallback_for_alpn"] = map[string]any{
			"http/1.1": fallback,
			"h2":       fallback,
		}
	}
	return candidate
}

func createSelfStealTLSRecord(tx *gorm.DB, profile selfStealProfileSpec, siteName string, handshakeHost string, target TargetView, privateKey string, publicKey string, shortID string) (model.Tls, error) {
	if profile.ID == selfStealProfileTrojan {
		return createSelfStealTrojanTLSRecord(tx, siteName, handshakeHost)
	}
	return createSelfStealRealityTLSRecord(tx, siteName, handshakeHost, target, privateKey, publicKey, shortID)
}

func createSelfStealRealityTLSRecord(tx *gorm.DB, siteName string, handshakeHost string, target TargetView, privateKey string, publicKey string, shortID string) (model.Tls, error) {
	server, err := json.Marshal(map[string]any{
		"enabled":     true,
		"server_name": handshakeHost,
		"reality": map[string]any{
			"enabled": true,
			"handshake": map[string]any{
				"server":      selfStealHandshakeServer(target),
				"server_port": target.Port,
			},
			"private_key": privateKey,
			"short_id":    []string{shortID},
		},
	})
	if err != nil {
		return model.Tls{}, err
	}
	client, err := json.Marshal(map[string]any{
		"enabled":     true,
		"server_name": handshakeHost,
		"utls": map[string]any{
			"enabled":     true,
			"fingerprint": "chrome",
		},
		"reality": map[string]any{
			"enabled":    true,
			"public_key": publicKey,
			"short_id":   shortID,
		},
	})
	if err != nil {
		return model.Tls{}, err
	}
	return createSelfStealTLSRow(tx, siteName, "fallback-html self-steal REALITY", server, client)
}

func createSelfStealTrojanTLSRecord(tx *gorm.DB, siteName string, handshakeHost string) (model.Tls, error) {
	certFile, keyFile := runtimeTLSFiles()
	if certFile == "" || keyFile == "" {
		return model.Tls{}, errors.New("panel certificate and key are required for Trojan TLS fallback")
	}
	server, err := json.Marshal(map[string]any{
		"enabled":          true,
		"server_name":      handshakeHost,
		"certificate_path": certFile,
		"key_path":         keyFile,
		"alpn":             []string{"h2", "http/1.1"},
	})
	if err != nil {
		return model.Tls{}, err
	}
	client, err := json.Marshal(map[string]any{
		"enabled":     true,
		"server_name": handshakeHost,
		"utls": map[string]any{
			"enabled":     true,
			"fingerprint": "chrome",
		},
	})
	if err != nil {
		return model.Tls{}, err
	}
	return createSelfStealTLSRow(tx, siteName, "fallback-html self-steal Trojan", server, client)
}

func createSelfStealTLSRow(tx *gorm.DB, siteName string, baseName string, server []byte, client []byte) (model.Tls, error) {
	name := baseName
	if trimmed := strings.TrimSpace(siteName); trimmed != "" {
		name += " - " + trimmed
	}
	row := model.Tls{
		Name:   name,
		Server: server,
		Client: client,
	}
	if err := tx.Create(&row).Error; err != nil {
		return model.Tls{}, err
	}
	return row, nil
}

func selfStealProfileSpecFor(profile string) selfStealProfileSpec {
	switch profile {
	case selfStealProfileTrojan:
		return selfStealProfileSpec{ID: selfStealProfileTrojan, InboundType: "trojan", TLSMode: "tls-fallback"}
	default:
		return selfStealProfileSpec{ID: selfStealProfileVLESS, InboundType: "vless", TLSMode: "reality"}
	}
}

func normalizeSelfStealTransport(value string, site fallbackdomain.Site, handshakeHost string) (string, map[string]any, error) {
	transport := strings.TrimSpace(strings.ToLower(value))
	if transport == "" {
		transport = selfStealDefaultTransport
	}
	host := strings.TrimSpace(handshakeHost)
	if host == "" {
		host = strings.TrimSpace(site.Hostname)
	}
	switch transport {
	case "tcp", "default":
		return selfStealDefaultTransport, nil, nil
	case "ws", "websocket":
		cfg := map[string]any{"type": "ws", "path": "/ws"}
		if host != "" {
			cfg["headers"] = map[string]any{"Host": host}
		}
		return "ws", cfg, nil
	case "http":
		cfg := map[string]any{"type": "http", "path": "/"}
		if host != "" {
			cfg["host"] = []string{host}
		}
		return "http", cfg, nil
	case "grpc":
		return "grpc", map[string]any{"type": "grpc", "service_name": "fallback"}, nil
	case "httpupgrade", "http-upgrade":
		cfg := map[string]any{"type": "httpupgrade", "path": "/"}
		if host != "" {
			cfg["host"] = host
		}
		return "httpupgrade", cfg, nil
	default:
		return "", nil, fmt.Errorf("unsupported self-steal transport %q", value)
	}
}

func resolveSelfStealPublicListen(value string, site fallbackdomain.Site, target TargetView) (string, []string, []string) {
	warnings := []string{}
	blocks := []string{}
	candidates := []string{
		value,
		site.Hostname,
		target.Host,
		target.Listen,
	}
	for _, candidate := range candidates {
		listen := normalizeListenAddress(candidate)
		if listen == "" {
			continue
		}
		if isWildcardListen(listen) {
			continue
		}
		if isLoopbackListen(listen) {
			warnings = append(warnings, "public self-steal listen resolves to loopback; this is useful for local testing but not for production")
		}
		return listen, warnings, blocks
	}
	blocks = append(blocks, "set an exact public listen address or domain for self-steal; wildcard 0.0.0.0/:: is not accepted")
	return "", warnings, blocks
}

func normalizeListenAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	return strings.TrimSpace(value)
}

func isWildcardListen(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "0.0.0.0" || value == "::" || value == "[::]"
}

func isLoopbackListen(value string) bool {
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(value, "localhost")
}

func selfStealTargetUsable(target TargetView) bool {
	switch strings.TrimSpace(target.Status) {
	case "", "available", "active", "managed", "free", "planned":
		return true
	default:
		return false
	}
}

func planSelfStealPortTransfer(target TargetView) PortOwnershipTransfer {
	if target.Port != selfStealPublicPort {
		return PortOwnershipTransfer{}
	}
	nextPort := selfStealDefaultLocalPort
	status, _ := probeBind(selfStealDefaultLocalHost, nextPort)
	if status != "free" {
		if candidate, ok := freeLoopbackPortCandidate(true); ok {
			nextPort = candidate.Port
		}
	}
	next := target
	next.Kind = "standalone"
	next.Host = target.Host
	next.Listen = selfStealDefaultLocalHost
	next.Port = nextPort
	next.Runtime = "gin"
	next.TLS = true
	next.Status = "planned"
	next.Reason = "local TLS fallback target prepared for self-steal port ownership transfer"
	return PortOwnershipTransfer{
		Required:       true,
		Reason:         "public 443 must be owned by the reviewed inbound; fallback-html must move the site behind that inbound",
		PreviousTarget: target,
		NewTarget:      next,
	}
}

func saveSelfStealTransferTarget(tx *gorm.DB, siteID uint, target TargetView, now int64) (TargetView, error) {
	var row fallbackdomain.RuntimeTarget
	if target.ID != 0 {
		if err := tx.Where("site_id = ?", siteID).First(&row, target.ID).Error; err != nil {
			return TargetView{}, err
		}
	} else {
		row = fallbackdomain.RuntimeTarget{SiteID: siteID, CreatedAt: now}
	}
	row.Kind = "standalone"
	row.Host = target.Host
	row.Listen = target.Listen
	row.Port = target.Port
	row.RootPath = target.RootPath
	if strings.TrimSpace(row.RootPath) == "" {
		row.RootPath = "/"
	}
	row.Runtime = "gin"
	row.TLS = true
	row.UpdatedAt = now
	if err := tx.Save(&row).Error; err != nil {
		return TargetView{}, err
	}
	if err := tx.Where("site_id = ? AND id <> ?", siteID, row.ID).Delete(&fallbackdomain.RuntimeTarget{}).Error; err != nil {
		return TargetView{}, err
	}
	target.ID = row.ID
	target.Kind = row.Kind
	target.Listen = row.Listen
	target.Port = row.Port
	target.RootPath = row.RootPath
	target.Runtime = row.Runtime
	target.TLS = row.TLS
	target.Status = "active"
	target.Reason = "fallback-html target prepared for self-steal handoff"
	return target, nil
}

func selfStealHandshakeServer(target TargetView) string {
	listen := strings.TrimSpace(target.Listen)
	switch listen {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return listen
	}
}

func generateSelfStealRealityKeyPair() (string, string, error) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", err
	}
	publicKey := privateKey.PublicKey()
	return base64.RawURLEncoding.EncodeToString(privateKey[:]), base64.RawURLEncoding.EncodeToString(publicKey[:]), nil
}

func generateSelfStealShortID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
