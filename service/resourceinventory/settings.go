package resourceinventory

import (
	"context"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type settingsReader interface {
	GetListen() (string, error)
	GetPort() (int, error)
	GetWebDomain() (string, error)
	GetWebPath() (string, error)
	GetCertFile() (string, error)
	GetKeyFile() (string, error)
	GetSubListen() (string, error)
	GetSubPort() (int, error)
	GetSubDomain() (string, error)
	GetSubPath() (string, error)
	GetSubCertFile() (string, error)
	GetSubKeyFile() (string, error)
}

type panelContributor struct{ settings settingsReader }

func (panelContributor) Owner() string { return "core" }

func (c panelContributor) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	listen, err := c.settings.GetListen()
	if err != nil {
		return nil, fmt.Errorf("read panel listen: %w", err)
	}
	port, err := c.settings.GetPort()
	if err != nil {
		return nil, fmt.Errorf("read panel port: %w", err)
	}
	domain, err := c.settings.GetWebDomain()
	if err != nil {
		return nil, fmt.Errorf("read panel domain: %w", err)
	}
	path, err := c.settings.GetWebPath()
	if err != nil {
		return nil, fmt.Errorf("read panel path: %w", err)
	}
	certFile, err := c.settings.GetCertFile()
	if err != nil {
		return nil, fmt.Errorf("read panel certificate setting: %w", err)
	}
	keyFile, err := c.settings.GetKeyFile()
	if err != nil {
		return nil, fmt.Errorf("read panel key setting: %w", err)
	}
	return []hostresources.ProtectableResource{listenerFromSettings(listenerSettings{
		id:         "core:panel:web",
		kind:       "panel_web",
		name:       "Panel web listener",
		listen:     listen,
		port:       port,
		domain:     domain,
		path:       path,
		certFile:   certFile,
		keyFile:    keyFile,
		routeHints: []string{"shares-listener:web-publicsurface", "surface:panel"},
		fallback:   hostresources.CapabilityYes,
	})}, nil
}

type subscriptionContributor struct{ settings settingsReader }

func (subscriptionContributor) Owner() string { return "core" }

func (c subscriptionContributor) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	listen, err := c.settings.GetSubListen()
	if err != nil {
		return nil, fmt.Errorf("read subscription listen: %w", err)
	}
	port, err := c.settings.GetSubPort()
	if err != nil {
		return nil, fmt.Errorf("read subscription port: %w", err)
	}
	domain, err := c.settings.GetSubDomain()
	if err != nil {
		return nil, fmt.Errorf("read subscription domain: %w", err)
	}
	path, err := c.settings.GetSubPath()
	if err != nil {
		return nil, fmt.Errorf("read subscription path: %w", err)
	}
	certFile, err := c.settings.GetSubCertFile()
	if err != nil {
		return nil, fmt.Errorf("read subscription certificate setting: %w", err)
	}
	keyFile, err := c.settings.GetSubKeyFile()
	if err != nil {
		return nil, fmt.Errorf("read subscription key setting: %w", err)
	}
	return []hostresources.ProtectableResource{listenerFromSettings(listenerSettings{
		id:         "core:subscription:default",
		kind:       "subscription",
		name:       "Subscription listener",
		listen:     listen,
		port:       port,
		domain:     domain,
		path:       path,
		certFile:   certFile,
		keyFile:    keyFile,
		routeHints: []string{"surface:subscription"},
		fallback:   hostresources.CapabilityNo,
	})}, nil
}

type listenerSettings struct {
	id         string
	kind       string
	name       string
	listen     string
	port       int
	domain     string
	path       string
	certFile   string
	keyFile    string
	routeHints []string
	fallback   hostresources.CapabilityValue
}

func listenerFromSettings(value listenerSettings) hostresources.ProtectableResource {
	normalized := hostresources.NormalizeListen(value.listen)
	tlsEnabled := strings.TrimSpace(value.certFile) != "" && strings.TrimSpace(value.keyFile) != ""
	warnings := make([]string, 0, 2)
	if (strings.TrimSpace(value.certFile) == "") != (strings.TrimSpace(value.keyFile) == "") {
		warnings = append(warnings, "TLS certificate and key settings are incomplete")
	}
	routeHints := append([]string(nil), value.routeHints...)
	if strings.TrimSpace(value.path) != "" {
		routeHints = append(routeHints, "path:configured")
	}
	hostnames := []string(nil)
	if domain := strings.TrimSpace(value.domain); domain != "" {
		hostnames = []string{domain}
	}
	revision := hostresources.Revision(struct {
		Listen   string
		Port     int
		Domain   string
		PathHash string
		TLS      bool
	}{normalized.Value, value.port, strings.ToLower(strings.TrimSpace(value.domain)), hostresources.Revision(value.path), tlsEnabled})
	expectedOwner := expectedApplicationListenerOwner()
	return hostresources.ProtectableResource{
		ID:       value.id,
		Kind:     value.kind,
		Owner:    "core",
		Name:     value.name,
		Protocol: "http",
		Listen:   normalized.Value,
		Port:     value.port,
		Public:   normalized.Public(),
		TLS:      tlsEnabled,
		Source:   "settings",
		Capabilities: hostresources.ProtectableResourceCapabilities{
			Known:                 true,
			AcceptsProxyProtocol:  hostresources.CapabilityNo,
			SupportsGracefulDrain: hostresources.CapabilityUnknown,
			CanServeFallback:      value.fallback,
			RequiresACMEHTTP01:    hostresources.CapabilityUnknown,
			RequiresTLSALPN01:     hostresources.CapabilityUnknown,
			PublicHostnames:       hostnames,
			RouteHints:            routeHints,
			TLSMode:               tlsMode(tlsEnabled),
			OwnerRevision:         revision,
			ConfigRevision:        revision,
			ExpectedListenerOwner: expectedOwner,
		},
		Warnings: warnings,
	}
}

func expectedApplicationListenerOwner() hostresources.ExpectedListenerOwnerV1 {
	contract, err := deploymentidentity.LoadInstalled()
	if err != nil {
		return hostresources.ExpectedListenerOwnerV1{}
	}
	return hostresources.ExpectedListenerOwnerV1{
		Schema: hostresources.ExpectedListenerOwnerSchemaV1, ContractRevision: contract.Revision,
		InstanceID: contract.InstanceID, SourceRevision: contract.SourceRevision,
		ArtifactRevision: contract.ArtifactRevision, DeploymentID: contract.DeploymentID,
		RuntimeRootBindingRevision: contract.RuntimeRootBindingRevision,
		ServiceIdentity:            contract.ServiceIdentity, SystemdUnit: contract.SystemdUnit,
		ServiceFragmentPath: contract.ServiceFragmentPath, ServiceUnitSHA256: contract.ServiceUnitSHA256,
		ServiceControlGroup: contract.ServiceControlGroup, ExecutablePath: contract.ExecutablePath,
		ExecutableSHA256: contract.ExecutableSHA256, ProcessUID: contract.ProcessUID, ProcessGID: contract.ProcessGID,
	}
}

func tlsMode(enabled bool) string {
	if enabled {
		return "configured"
	}
	return "disabled"
}
