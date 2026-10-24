// Package targetcheck defines the engine-free observation returned by a Panel
// candidate's fixed core-check command. It grants no installation authority.
package targetcheck

import (
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
)

const (
	MaxConfigBytes = 8 << 20
	MaxReportBytes = 64 << 10
	Schema         = "solovey.core-target-check/v1"
)

type Report struct {
	Schema              string                  `json:"schema"`
	State               string                  `json:"state"`
	CoreProvider        coreprovider.Identity   `json:"coreProvider"`
	PanelVersion        string                  `json:"panelVersion"`
	Capabilities        capabilityview.Snapshot `json:"capabilities"`
	CapabilityRevision  string                  `json:"capabilityRevision"`
	ConfigurationDigest string                  `json:"configurationDigest,omitempty"`
	ReasonCodes         []string                `json:"reasonCodes"`
}
