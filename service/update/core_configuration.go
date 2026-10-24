package update

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
)

// CoreConfigurationOwner is the existing ConfigService lifecycle boundary.
// Update owns admission and checkpoints; this port owns neither installation
// nor a second configuration store. Configuration bytes remain process-local.
type CoreConfigurationOwner interface {
	CaptureProviderConfiguration(context.Context) (CoreConfigurationSnapshot, error)
	SetCoreMaintenance(context.Context, string, bool) error
}

type PreservedConfiguration struct {
	Category string `json:"category"`
	ID       uint   `json:"id"`
	Type     string `json:"type"`
	Reason   string `json:"reason"`
}

type CoreConfigurationSnapshot struct {
	Configuration         []byte                   `json:"-"`
	ConfigurationDigest   string                   `json:"configurationDigest"`
	ConfigurationRevision string                   `json:"configurationRevision"`
	Maintenance           bool                     `json:"maintenance"`
	RuntimeGeneration     string                   `json:"runtimeGeneration"`
	RuntimeState          string                   `json:"runtimeState"`
	CapabilityRevision    string                   `json:"capabilityRevision"`
	Capabilities          capabilityview.Snapshot  `json:"capabilities"`
	Preserved             []PreservedConfiguration `json:"preserved"`
}

func (snapshot *CoreConfigurationSnapshot) Clear() {
	if snapshot != nil {
		clear(snapshot.Configuration)
		snapshot.Configuration = nil
	}
}
