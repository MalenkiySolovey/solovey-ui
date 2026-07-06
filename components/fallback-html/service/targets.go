//go:build !minimal

package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/network/bind"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

type TargetInput struct {
	ID       uint   `json:"id"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	RootPath string `json:"rootPath"`
	Runtime  string `json:"runtime"`
	TLS      bool   `json:"tls"`
}

type TargetView struct {
	ID       uint   `json:"id"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	RootPath string `json:"rootPath"`
	Runtime  string `json:"runtime"`
	TLS      bool   `json:"tls"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Current  bool   `json:"current"`
}

type PortCandidate struct {
	Kind    string `json:"kind"`
	Listen  string `json:"listen"`
	Port    int    `json:"port"`
	Runtime string `json:"runtime"`
	TLS     bool   `json:"tls"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
}

type RuntimeOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	NodeSide bool   `json:"nodeSide"`
}

func (s *Service) ListTargets(siteID uint) ([]TargetView, error) {
	var site fallbackdomain.Site
	if err := s.db.Preload("Targets", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).First(&site, siteID).Error; err != nil {
		return nil, err
	}
	return s.targetsForSite(site)
}

func (s *Service) SaveTarget(siteID uint, input TargetInput, actor string) (TargetView, error) {
	now := time.Now().Unix()
	current, err := s.currentWebTarget()
	if err != nil {
		return TargetView{}, err
	}
	if input.Kind == "" {
		input.Kind = current.Kind
	}
	if input.Kind != "web-current" {
		return TargetView{}, fmt.Errorf("only the current managed web listener is supported by the Gin MVP")
	}
	var saved fallbackdomain.RuntimeTarget
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
			return err
		}
		if input.ID != 0 {
			if err := tx.Where("site_id = ?", siteID).First(&saved, input.ID).Error; err != nil {
				return err
			}
		} else {
			saved = fallbackdomain.RuntimeTarget{SiteID: siteID, CreatedAt: now}
		}
		saved.Kind = current.Kind
		saved.Host = current.Host
		saved.Listen = current.Listen
		saved.Port = current.Port
		saved.RootPath = current.RootPath
		saved.Runtime = current.Runtime
		saved.TLS = current.TLS
		saved.UpdatedAt = now
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "target_saved", map[string]any{"kind": saved.Kind, "port": saved.Port})
	})
	if err != nil {
		return TargetView{}, err
	}
	return s.targetView(saved, current), nil
}

func (s *Service) DeleteTarget(siteID, targetID uint, actor string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var target fallbackdomain.RuntimeTarget
		if err := tx.Where("site_id = ?", siteID).First(&target, targetID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&target).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "target_deleted", map[string]any{"targetId": targetID})
	})
	if err != nil {
		return err
	}
	return s.runtime.Rebuild(s.db)
}

func (s *Service) PortCandidates() ([]PortCandidate, error) {
	current, err := s.currentWebTarget()
	if err != nil {
		return nil, err
	}
	currentCandidate := PortCandidate{
		Kind:    "web-current",
		Listen:  current.Listen,
		Port:    current.Port,
		Runtime: current.Runtime,
		TLS:     current.TLS,
		Status:  "managed",
		Reason:  "current panel web listener owns this port",
	}
	if fallback, reason := addressWouldFallback(current.Listen); fallback {
		currentCandidate.Status = "loopback-fallback"
		currentCandidate.Reason = reason
	}
	candidates := []PortCandidate{currentCandidate}
	candidates = append(candidates, s.savedTargetPortCandidates(current)...)
	candidates = append(candidates, s.inboundPortCandidates()...)
	if free, ok := freeLoopbackPortCandidate(current.TLS); ok {
		candidates = append(candidates, free)
	}
	sortPortCandidates(candidates)
	return candidates, nil
}

func (s *Service) RuntimeOptions() []RuntimeOption {
	return []RuntimeOption{
		{
			ID:     "gin",
			Label:  "Panel web listener",
			Status: "available",
			Reason: "Serves the published site through the existing panel web listener.",
		},
		{
			ID:       "nginx",
			Label:    "Nginx runtime",
			Status:   "unavailable",
			Reason:   "Node runtime capability is required; the panel component never runs nginx or systemctl directly.",
			NodeSide: true,
		},
		{
			ID:       "caddy",
			Label:    "Caddy runtime",
			Status:   "unavailable",
			Reason:   "Node runtime capability is required; the panel component never runs caddy or systemctl directly.",
			NodeSide: true,
		},
	}
}

func (s *Service) savedTargetPortCandidates(current TargetView) []PortCandidate {
	var targets []fallbackdomain.RuntimeTarget
	if err := s.db.Order("site_id ASC, id ASC").Find(&targets).Error; err != nil {
		return nil
	}
	out := make([]PortCandidate, 0, len(targets))
	seen := map[string]bool{
		portCandidateKey("web-current", current.Listen, current.Port): true,
		portCandidateKey("target", current.Listen, current.Port):      true,
	}
	for _, target := range targets {
		if target.Port <= 0 {
			continue
		}
		key := portCandidateKey("target", target.Listen, target.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		candidate := probeTargetPortCandidate(target, current)
		out = append(out, candidate)
	}
	return out
}

func (s *Service) inboundPortCandidates() []PortCandidate {
	var inbounds []model.Inbound
	if err := s.db.Find(&inbounds).Error; err != nil {
		return nil
	}
	out := make([]PortCandidate, 0, len(inbounds))
	seen := map[string]bool{}
	for _, inbound := range inbounds {
		listen, port, ok := inboundListen(inbound)
		if !ok || port <= 0 {
			continue
		}
		key := portCandidateKey("inbound", listen, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		tag := strings.TrimSpace(inbound.Tag)
		if tag == "" {
			tag = uintString(inbound.Id)
		}
		out = append(out, PortCandidate{
			Kind:    "inbound",
			Listen:  listen,
			Port:    port,
			Runtime: "sing-box",
			Status:  "blocked-inbound",
			Reason:  "sing-box inbound " + tag + " already uses this port",
		})
	}
	return out
}

func probeTargetPortCandidate(target fallbackdomain.RuntimeTarget, current TargetView) PortCandidate {
	candidate := PortCandidate{
		Kind:    target.Kind,
		Listen:  strings.TrimSpace(target.Listen),
		Port:    target.Port,
		Runtime: strings.TrimSpace(target.Runtime),
		TLS:     target.TLS,
	}
	if candidate.Kind == "" {
		candidate.Kind = "target"
	}
	if candidate.Runtime == "" {
		candidate.Runtime = "gin"
	}
	if targetMatchesCurrent(candidate.Listen, candidate.Port, candidate.Runtime, current) {
		candidate.Status = "managed"
		candidate.Reason = "current panel web listener owns this port"
		return candidate
	}
	status, reason := probeBind(candidate.Listen, candidate.Port)
	candidate.Status = status
	candidate.Reason = reason
	return candidate
}

func freeLoopbackPortCandidate(tls bool) (PortCandidate, bool) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return PortCandidate{}, false
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return PortCandidate{}, false
	}
	return PortCandidate{
		Kind:    "free",
		Listen:  "127.0.0.1",
		Port:    addr.Port,
		Runtime: "gin",
		TLS:     tls,
		Status:  "free",
		Reason:  "available on loopback at the time of the check",
	}, true
}

func probeBind(listen string, port int) (string, string) {
	host := strings.TrimSpace(listen)
	if host == "" {
		host = "127.0.0.1"
	}
	result, err := bind.ListenWithFallbackResult(net.JoinHostPort(host, strconv.Itoa(port)), host, strconv.Itoa(port))
	if err != nil {
		return "blocked-external", "could not bind this port during a non-destructive availability check"
	}
	defer result.Listener.Close()
	if result.Fallback {
		return "loopback-fallback", "requested listen address is not available on this host; panel would fall back to loopback"
	}
	return "free", "available at the time of the check"
}

func addressWouldFallback(listen string) (bool, string) {
	host := strings.TrimSpace(listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return false, ""
	}
	result, err := bind.ListenWithFallbackResult(net.JoinHostPort(host, "0"), host, "0")
	if err != nil {
		return false, ""
	}
	defer result.Listener.Close()
	if result.Fallback {
		return true, "configured listen address is not available on this host; panel would fall back to loopback"
	}
	return false, ""
}

func inboundListen(inbound model.Inbound) (string, int, bool) {
	var options map[string]any
	if len(inbound.Options) == 0 {
		return "", 0, false
	}
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		return "", 0, false
	}
	port, ok := intFromJSON(options["listen_port"])
	if !ok || port <= 0 {
		return "", 0, false
	}
	listen, _ := options["listen"].(string)
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = "0.0.0.0"
	}
	return listen, port, true
}

func intFromJSON(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		port := int(typed)
		return port, typed == float64(port)
	case int:
		return typed, true
	case json.Number:
		port, err := strconv.Atoi(typed.String())
		return port, err == nil
	case string:
		port, err := strconv.Atoi(strings.TrimSpace(typed))
		return port, err == nil
	default:
		return 0, false
	}
}

func targetMatchesCurrent(listen string, port int, runtime string, current TargetView) bool {
	return strings.TrimSpace(listen) == strings.TrimSpace(current.Listen) &&
		port == current.Port &&
		strings.TrimSpace(runtime) == strings.TrimSpace(current.Runtime)
}

func portCandidateKey(kind string, listen string, port int) string {
	return kind + "|" + strings.TrimSpace(listen) + "|" + strconv.Itoa(port)
}

func sortPortCandidates(candidates []PortCandidate) {
	priority := map[string]int{
		"managed":           0,
		"loopback-fallback": 1,
		"blocked-inbound":   2,
		"blocked-external":  3,
		"free":              4,
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if priority[left.Status] != priority[right.Status] {
			return priority[left.Status] < priority[right.Status]
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.Listen != right.Listen {
			return left.Listen < right.Listen
		}
		return left.Kind < right.Kind
	})
}

func (s *Service) targetsForSite(site fallbackdomain.Site) ([]TargetView, error) {
	current, err := s.currentWebTarget()
	if err != nil {
		return nil, err
	}
	if len(site.Targets) == 0 {
		return []TargetView{current}, nil
	}
	out := make([]TargetView, 0, len(site.Targets))
	for _, target := range site.Targets {
		out = append(out, s.targetView(target, current))
	}
	return out, nil
}

func normalizeSelfStealProfile(value string) (string, error) {
	profile := strings.TrimSpace(strings.ToLower(value))
	if profile == "" {
		profile = "sing-box-reality"
	}
	switch profile {
	case "sing-box-reality", "custom":
		return profile, nil
	default:
		return "", fmt.Errorf("unsupported self-steal profile %q", value)
	}
}

func selectSelfStealTarget(targets []TargetView, targetID uint) (TargetView, error) {
	if len(targets) == 0 {
		return TargetView{}, errors.New("site has no runtime target")
	}
	if targetID == 0 {
		return targets[0], nil
	}
	for _, target := range targets {
		if target.ID == targetID {
			return target, nil
		}
	}
	return TargetView{}, fmt.Errorf("fallback-html runtime target %d not found", targetID)
}

func normalizeSelfStealHandshakeHost(value string, target TargetView) string {
	host := strings.TrimSpace(value)
	if host != "" {
		return host
	}
	if strings.TrimSpace(target.Host) != "" {
		return strings.TrimSpace(target.Host)
	}
	if target.Listen != "" && target.Listen != "0.0.0.0" && target.Listen != "::" {
		return target.Listen
	}
	return "localhost"
}

func (s *Service) targetView(target fallbackdomain.RuntimeTarget, current TargetView) TargetView {
	view := TargetView{
		ID:       target.ID,
		Kind:     target.Kind,
		Host:     target.Host,
		Listen:   target.Listen,
		Port:     target.Port,
		RootPath: target.RootPath,
		Runtime:  target.Runtime,
		TLS:      target.TLS,
		Status:   "available",
		Reason:   "managed by the current panel web listener",
	}
	view.Current = view.Kind == current.Kind &&
		view.Listen == current.Listen &&
		view.Port == current.Port &&
		view.Runtime == current.Runtime &&
		view.RootPath == current.RootPath
	if !view.Current {
		view.Status = "stale"
		view.Reason = "target no longer matches the current panel web listener; save it again before publishing"
	}
	if view.Kind != "web-current" {
		view.Status = "unsupported"
		view.Reason = "this runtime target requires a later runtime adapter"
	}
	return view
}

func (s *Service) currentWebTarget() (TargetView, error) {
	settings := coreservice.SettingService{}
	listen, err := settings.GetListen()
	if err != nil {
		return TargetView{}, err
	}
	port, err := settings.GetPort()
	if err != nil {
		return TargetView{}, err
	}
	host, err := settings.GetWebDomain()
	if err != nil {
		return TargetView{}, err
	}
	certFile, err := settings.GetCertFile()
	if err != nil {
		return TargetView{}, err
	}
	keyFile, err := settings.GetKeyFile()
	if err != nil {
		return TargetView{}, err
	}
	return TargetView{
		Kind:     "web-current",
		Host:     strings.TrimSpace(host),
		Listen:   strings.TrimSpace(listen),
		Port:     port,
		RootPath: "/",
		Runtime:  "gin",
		TLS:      certFile != "" || keyFile != "",
		Status:   "available",
		Reason:   "uses the current managed panel web listener",
		Current:  true,
	}, nil
}

func targetFromView(siteID uint, view TargetView, now int64) fallbackdomain.RuntimeTarget {
	return fallbackdomain.RuntimeTarget{
		SiteID:    siteID,
		Kind:      view.Kind,
		Host:      view.Host,
		Listen:    view.Listen,
		Port:      view.Port,
		RootPath:  view.RootPath,
		Runtime:   view.Runtime,
		TLS:       view.TLS,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
