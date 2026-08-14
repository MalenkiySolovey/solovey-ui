//go:build !minimal

package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
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
	target, err := normalizeTargetInput(siteID, input, current, now)
	if err != nil {
		return TargetView{}, err
	}
	var saved fallbackdomain.RuntimeTarget
	var site fallbackdomain.Site
	err = s.guardedRuntimeMutation(siteID, func(tx *gorm.DB) error {
		if err := tx.First(&site, siteID).Error; err != nil {
			return err
		}
		if input.ID != 0 {
			if err := tx.Where("site_id = ?", siteID).First(&saved, input.ID).Error; err != nil {
				return err
			}
		} else {
			saved = fallbackdomain.RuntimeTarget{SiteID: siteID, CreatedAt: now}
		}
		saved.Kind = target.Kind
		saved.Host = target.Host
		saved.Listen = target.Listen
		saved.Port = target.Port
		saved.RootPath = target.RootPath
		saved.Runtime = target.Runtime
		saved.TLS = target.TLS
		saved.UpdatedAt = now
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if err := tx.Where("site_id = ? AND id <> ?", siteID, saved.ID).Delete(&fallbackdomain.RuntimeTarget{}).Error; err != nil {
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
	return s.guardedRuntimeMutation(siteID, func(tx *gorm.DB) error {
		var target fallbackdomain.RuntimeTarget
		if err := tx.Where("site_id = ?", siteID).First(&target, targetID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&target).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "target_deleted", map[string]any{"targetId": targetID})
	})
}

func (s *Service) PortCandidates(ctx context.Context) ([]PortCandidate, error) {
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
	if candidate, ok := defaultHTTPSPortCandidate(current); ok {
		candidates = append(candidates, candidate)
	}
	saved, err := s.savedTargetPortCandidates(current)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, saved...)
	inbounds, err := s.inboundPortCandidates(ctx)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, inbounds...)
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
	}
}

func normalizeTargetInput(siteID uint, input TargetInput, current TargetView, now int64) (fallbackdomain.RuntimeTarget, error) {
	listen := strings.TrimSpace(input.Listen)
	if listen == "" {
		listen = strings.TrimSpace(input.Host)
	}
	if listen == "" {
		listen = "127.0.0.1"
	}
	port := input.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return fallbackdomain.RuntimeTarget{}, fmt.Errorf("publish port must be between 1 and 65535")
	}
	rootPath := strings.TrimSpace(input.RootPath)
	if rootPath == "" {
		rootPath = "/"
	}
	runtime := strings.TrimSpace(input.Runtime)
	if runtime == "" {
		runtime = "gin"
	}
	if runtime != "gin" {
		return fallbackdomain.RuntimeTarget{}, fmt.Errorf("runtime %q requires a node/runtime component", runtime)
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "standalone"
	}
	if kind == "web-current" {
		return targetFromView(siteID, current, now), nil
	}
	if targetMatchesCurrent(listen, port, runtime, current) {
		kind = "web-current"
	}
	switch kind {
	case "web-current":
		return fallbackdomain.RuntimeTarget{
			SiteID:    siteID,
			Kind:      current.Kind,
			Host:      current.Host,
			Listen:    current.Listen,
			Port:      current.Port,
			RootPath:  current.RootPath,
			Runtime:   current.Runtime,
			TLS:       current.TLS,
			CreatedAt: now,
			UpdatedAt: now,
		}, nil
	case "standalone":
		return fallbackdomain.RuntimeTarget{
			SiteID:    siteID,
			Kind:      "standalone",
			Host:      strings.TrimSpace(input.Host),
			Listen:    listen,
			Port:      port,
			RootPath:  rootPath,
			Runtime:   runtime,
			TLS:       input.TLS,
			CreatedAt: now,
			UpdatedAt: now,
		}, nil
	default:
		return fallbackdomain.RuntimeTarget{}, fmt.Errorf("unsupported publish target kind %q", kind)
	}
}

func defaultHTTPSPortCandidate(current TargetView) (PortCandidate, bool) {
	listen := preferredExactListen(current)
	if current.Port == 443 && listen == strings.TrimSpace(current.Listen) {
		return PortCandidate{}, false
	}
	candidate := PortCandidate{
		Kind:    "standalone",
		Listen:  listen,
		Port:    443,
		Runtime: "gin",
		TLS:     current.TLS,
	}
	status, reason := probeBind(candidate.Listen, candidate.Port)
	candidate.Status = status
	candidate.Reason = reason
	return candidate, true
}

func (s *Service) savedTargetPortCandidates(current TargetView) ([]PortCandidate, error) {
	var targets []fallbackdomain.RuntimeTarget
	if err := s.db.Order("site_id ASC, id ASC").Find(&targets).Error; err != nil {
		return nil, err
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
	return out, nil
}

func (s *Service) inboundPortCandidates(ctx context.Context) ([]PortCandidate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshotter := s.resourceSnapshot
	if snapshotter == nil {
		snapshotter = hostresources.Snapshot
	}
	snapshot := snapshotter(ctx)
	if len(snapshot.Errors) > 0 {
		return nil, errors.New("host resource inventory is unavailable")
	}
	out := make([]PortCandidate, 0)
	seen := map[string]bool{}
	for _, inbound := range snapshot.Resources {
		if inbound.Owner != "core" || inbound.Kind != "inbound" || inbound.Port <= 0 {
			continue
		}
		key := portCandidateKey("inbound", inbound.Listen, inbound.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		tag := strings.TrimSpace(inbound.InboundTag)
		if tag == "" {
			tag = strings.TrimSpace(inbound.Name)
		}
		if tag == "" {
			tag = inbound.ID
		}
		out = append(out, PortCandidate{
			Kind:    "inbound",
			Listen:  inbound.Listen,
			Port:    inbound.Port,
			Runtime: "sing-box",
			Status:  "blocked-inbound",
			Reason:  "sing-box inbound " + tag + " already uses this port",
		})
	}
	return out, nil
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

func preferredExactListen(current TargetView) string {
	for _, candidate := range []string{current.Host, current.Listen} {
		listen := strings.TrimSpace(candidate)
		if listen != "" && listen != "0.0.0.0" && listen != "::" && listen != "[::]" {
			return listen
		}
	}
	return "127.0.0.1"
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
	if view.Kind == "standalone" {
		if view.Port <= 0 {
			view.Status = "blocked-external"
			view.Reason = "port is required"
			return view
		}
		if view.TLS {
			certFile, keyFile := runtimeTLSFiles()
			if certFile == "" || keyFile == "" {
				view.Status = "blocked-external"
				view.Reason = "TLS target requires panel certificate and key settings"
				return view
			}
		}
		if s.runtime != nil && s.runtime.Owns(view.Listen, view.Port) {
			view.Status = "active"
			view.Reason = "fallback-html currently owns this listener"
			return view
		}
		view.Status, view.Reason = probeBind(view.Listen, view.Port)
		return view
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
		TLS:      certFile != "" && keyFile != "",
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
