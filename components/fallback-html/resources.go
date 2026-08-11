//go:build !minimal

package fallbackhtml

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

type publicSiteResourceContributor struct{ db *gorm.DB }

func (publicSiteResourceContributor) Owner() string { return id }

func (c publicSiteResourceContributor) ListProtectableResources(ctx context.Context) ([]hostresources.ProtectableResource, error) {
	if c.db == nil {
		return nil, fmt.Errorf("fallback-html database is unavailable")
	}
	var sites []fallbackdomain.Site
	err := c.db.WithContext(ctx).
		Preload("Targets", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("enabled = ? AND status = ?", true, "published").
		Order("id ASC").
		Find(&sites).Error
	if err != nil {
		return nil, fmt.Errorf("list published fallback sites: %w", err)
	}
	result := make([]hostresources.ProtectableResource, 0, len(sites))
	for _, site := range sites {
		target, warnings, err := siteResourceTarget(site)
		if err != nil {
			return nil, fmt.Errorf("resolve public site %d target: %w", site.ID, err)
		}
		result = append(result, publicSiteResource(site, target, warnings))
	}
	return result, nil
}

func siteResourceTarget(site fallbackdomain.Site) (fallbackdomain.RuntimeTarget, []string, error) {
	if len(site.Targets) > 0 {
		targets := append([]fallbackdomain.RuntimeTarget(nil), site.Targets...)
		sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
		warnings := []string(nil)
		if len(targets) > 1 {
			warnings = append(warnings, "site has multiple legacy runtime targets; inventory uses the first stable target")
		}
		return targets[0], warnings, nil
	}
	settings := &coreservice.SettingService{}
	listen, err := settings.GetListen()
	if err != nil {
		return fallbackdomain.RuntimeTarget{}, nil, err
	}
	port, err := settings.GetPort()
	if err != nil {
		return fallbackdomain.RuntimeTarget{}, nil, err
	}
	host, err := settings.GetWebDomain()
	if err != nil {
		return fallbackdomain.RuntimeTarget{}, nil, err
	}
	certFile, err := settings.GetCertFile()
	if err != nil {
		return fallbackdomain.RuntimeTarget{}, nil, err
	}
	keyFile, err := settings.GetKeyFile()
	if err != nil {
		return fallbackdomain.RuntimeTarget{}, nil, err
	}
	return fallbackdomain.RuntimeTarget{
		SiteID:   site.ID,
		Kind:     "web-current",
		Host:     strings.TrimSpace(host),
		Listen:   strings.TrimSpace(listen),
		Port:     port,
		RootPath: "/",
		Runtime:  "gin",
		TLS:      strings.TrimSpace(certFile) != "" && strings.TrimSpace(keyFile) != "",
	}, []string{"site uses the current panel listener because no explicit runtime target is stored"}, nil
}

func publicSiteResource(site fallbackdomain.Site, target fallbackdomain.RuntimeTarget, warnings []string) hostresources.ProtectableResource {
	normalized := hostresources.NormalizeListen(target.Listen)
	hostnames := make([]string, 0, 2)
	for _, hostname := range []string{site.Hostname, target.Host} {
		if hostname = strings.TrimSpace(hostname); hostname != "" {
			hostnames = append(hostnames, hostname)
		}
	}
	routeHints := []string{"surface:public-site", "path:configured"}
	if strings.EqualFold(strings.TrimSpace(target.Kind), "web-current") {
		routeHints = append(routeHints, "shares-listener:web-publicsurface")
	}
	revision := hostresources.Revision(struct {
		SiteID    uint
		TargetID  uint
		Kind      string
		Listen    string
		Port      int
		TLS       bool
		UpdatedAt int64
	}{site.ID, target.ID, target.Kind, normalized.Value, target.Port, target.TLS, target.UpdatedAt})
	name := strings.TrimSpace(site.Name)
	if name == "" {
		name = "Public site " + strconv.FormatUint(uint64(site.ID), 10)
	}
	return hostresources.ProtectableResource{
		ID:          "component:fallback-html:site:" + strconv.FormatUint(uint64(site.ID), 10),
		Kind:        "public_site",
		Owner:       id,
		Name:        name,
		Protocol:    "http",
		Listen:      normalized.Value,
		Port:        target.Port,
		Public:      normalized.Public(),
		TLS:         target.TLS,
		Source:      "component",
		ComponentID: id,
		Capabilities: hostresources.ProtectableResourceCapabilities{
			Known:                 target.Port > 0,
			AcceptsProxyProtocol:  hostresources.CapabilityNo,
			SupportsGracefulDrain: hostresources.CapabilityUnknown,
			CanServeFallback:      hostresources.CapabilityYes,
			RequiresACMEHTTP01:    hostresources.CapabilityUnknown,
			RequiresTLSALPN01:     hostresources.CapabilityUnknown,
			PublicHostnames:       hostnames,
			RouteHints:            routeHints,
			TLSMode:               fallbackTLSMode(target.TLS),
			OwnerRevision:         revision,
			ConfigRevision:        revision,
		},
		Warnings: warnings,
	}
}

func fallbackTLSMode(enabled bool) string {
	if enabled {
		return "configured"
	}
	return "disabled"
}
