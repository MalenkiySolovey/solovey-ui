package resources

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

type ExpectedApplicationOwnerV1 = deploymentidentity.ExpectedApplicationOwnerV1

const ExpectedApplicationOwnerSchemaV1 = deploymentidentity.ExpectedApplicationOwnerSchemaV1

type CapabilityValue string

const (
	CapabilityUnknown CapabilityValue = "unknown"
	CapabilityYes     CapabilityValue = "yes"
	CapabilityNo      CapabilityValue = "no"
)

type ProtectableResourceCapabilities struct {
	Known                    bool                                          `json:"known"`
	AcceptsProxyProtocol     CapabilityValue                               `json:"acceptsProxyProtocol"`
	SupportsGracefulDrain    CapabilityValue                               `json:"supportsGracefulDrain"`
	CanServeFallback         CapabilityValue                               `json:"canServeFallback"`
	RequiresACMEHTTP01       CapabilityValue                               `json:"requiresAcmeHttp01"`
	RequiresTLSALPN01        CapabilityValue                               `json:"requiresTlsAlpn01"`
	PublicHostnames          []string                                      `json:"publicHostnames,omitempty"`
	RouteHints               []string                                      `json:"routeHints,omitempty"`
	TLSMode                  string                                        `json:"tlsMode,omitempty"`
	FallbackTargetID         string                                        `json:"fallbackTargetId,omitempty"`
	OwnerRevision            string                                        `json:"ownerRevision,omitempty"`
	ConfigRevision           string                                        `json:"configRevision,omitempty"`
	ExpectedApplicationOwner deploymentidentity.ExpectedApplicationOwnerV1 `json:"expectedApplicationOwner,omitempty"`
}

type ProtectableResource struct {
	ID                  string                          `json:"id"`
	Kind                string                          `json:"kind"`
	Owner               string                          `json:"owner"`
	Name                string                          `json:"name"`
	Protocol            string                          `json:"protocol"`
	Listen              string                          `json:"listen"`
	Port                int                             `json:"port"`
	Public              bool                            `json:"public"`
	TLS                 bool                            `json:"tls"`
	Source              string                          `json:"source"`
	InboundTag          string                          `json:"inboundTag,omitempty"`
	ComponentID         string                          `json:"componentId,omitempty"`
	Fingerprint         string                          `json:"fingerprint"`
	Capabilities        ProtectableResourceCapabilities `json:"capabilities"`
	ListenIntent        ConfiguredListenIntentV1        `json:"listenIntent"`
	ListenIntents       []ConfiguredListenIntentV1      `json:"listenIntents,omitempty"`
	Endpoints           []PublicEndpoint                `json:"endpoints"`
	SocketCoverage      *SocketCoverageV1               `json:"socketCoverage,omitempty"`
	ManagementEndpoints []ManagementEndpointV1          `json:"managementEndpoints,omitempty"`
	AdvertisedEndpoints []AdvertisedEndpoint            `json:"advertisedEndpoints,omitempty"`
	Warnings            []string                        `json:"warnings,omitempty"`
}

type ResourceWarning struct {
	Owner      string `json:"owner"`
	ResourceID string `json:"resourceId,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type ResourceError struct {
	Owner   string `json:"owner"`
	Message string `json:"message"`
}

type ResourceSnapshot struct {
	GeneratedAt int64                 `json:"generatedAt"`
	Resources   []ProtectableResource `json:"resources"`
	Warnings    []ResourceWarning     `json:"warnings,omitempty"`
	Errors      []ResourceError       `json:"errors,omitempty"`
}

type ResourceContributor interface {
	Owner() string
	ListProtectableResources(context.Context) ([]ProtectableResource, error)
}
