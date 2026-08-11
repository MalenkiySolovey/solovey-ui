// Package fronting contains the local L4 fronting contract. Preview remains
// pure; every mutation responsibility is available only through Workflow and
// the restricted typed helper.
package fronting

import "context"

type DetectionState string

const (
	StateSupported               DetectionState = "supported"
	StateNotFound                DetectionState = "not_found"
	StateMultipleBinaries        DetectionState = "multiple_binaries"
	StateVersionUnknown          DetectionState = "version_unknown"
	StateMissingStream           DetectionState = "missing_stream"
	StateMissingSSLPreread       DetectionState = "missing_ssl_preread"
	StateDynamicModuleUnresolved DetectionState = "dynamic_module_unresolved"
	StateExternalManaged         DetectionState = "external_managed"
	StateConfigRootUnknown       DetectionState = "config_root_unknown"
	StateIncludeNotControlled    DetectionState = "include_not_controlled"
	StatePermissionDenied        DetectionState = "permission_denied"
	StateTimeout                 DetectionState = "timeout"
	StateMalformedOutput         DetectionState = "malformed_output"
	StateOversizedOutput         DetectionState = "oversized_output"
	StateUnsupported             DetectionState = "unsupported"
	StateUnknown                 DetectionState = "unknown"
)

type Availability string

const (
	AvailabilitySupported   Availability = "supported"
	AvailabilityUnsupported Availability = "unsupported"
	AvailabilityUnknown     Availability = "unknown"
)

type BinaryIdentity struct {
	Path       string `json:"path,omitempty"`
	TargetPath string `json:"targetPath,omitempty"`
	Device     uint64 `json:"device,omitempty"`
	Inode      uint64 `json:"inode,omitempty"`
	PlatformID string `json:"platformId,omitempty"`
}

type BuildFacts struct {
	Version                   string   `json:"version,omitempty"`
	ConfigPath                string   `json:"configPath,omitempty"`
	ConfigureArguments        []string `json:"configureArguments,omitempty"`
	StreamBuiltIn             bool     `json:"streamBuiltIn"`
	SSLPrereadBuiltIn         bool     `json:"sslPrereadBuiltIn"`
	StreamDynamicDeclared     bool     `json:"streamDynamicDeclared"`
	SSLPrereadDynamicDeclared bool     `json:"sslPrereadDynamicDeclared"`
	DynamicModuleAvailable    bool     `json:"dynamicModuleAvailable"`
	ModuleRoots               []string `json:"moduleRoots,omitempty"`
}

type ConfigOwnership struct {
	ConfigRoot        string `json:"configRoot,omitempty"`
	ManagedRoot       string `json:"managedRoot,omitempty"`
	ControlledInclude string `json:"controlledInclude,omitempty"`
	ExternalManaged   bool   `json:"externalManaged"`
}

type CapabilityReport struct {
	AdapterID          string          `json:"adapterId"`
	AdapterKind        string          `json:"adapterKind"`
	CapabilityRevision string          `json:"capabilityRevision"`
	State              DetectionState  `json:"state"`
	Supported          bool            `json:"supported"`
	Binary             BinaryIdentity  `json:"binary"`
	Build              BuildFacts      `json:"build"`
	Ownership          ConfigOwnership `json:"ownership"`
	ManagedRoot        Availability    `json:"managedRoot"`
	ControlledInclude  Availability    `json:"controlledInclude"`
	Validate           Availability    `json:"validate"`
	Activate           Availability    `json:"activate"`
	Reload             Availability    `json:"reload"`
	SNI                Availability    `json:"sni"`
	ALPN               Availability    `json:"alpn"`
	ProxyProtocol      Availability    `json:"proxyProtocol"`
	Warnings           []string        `json:"warnings"`
	Diagnostics        []string        `json:"diagnostics"`
	Reason             string          `json:"reason,omitempty"`
	CurrentRevision    string          `json:"currentRevision,omitempty"`
	ActiveRevision     string          `json:"activeRevision,omitempty"`
	ActiveSHA256       string          `json:"activeSha256,omitempty"`
}

// FrontingAdapter is component-local by design. Core and sibling components
// continue to depend only on their existing generic host registries.
type FrontingAdapter interface {
	ID() string
	Kind() string
	Capability(context.Context) CapabilityReport
	Preview(context.Context, PreviewInput) (Preview, error)
}

type FrontingController interface {
	FrontingAdapter
	Sync(context.Context, SyncInput) (WorkflowResult, error)
	Apply(context.Context, ApplyInput) (WorkflowResult, error)
	Rollback(context.Context, string, string) (WorkflowResult, error)
	Operation(context.Context, string) (WorkflowResult, error)
}
