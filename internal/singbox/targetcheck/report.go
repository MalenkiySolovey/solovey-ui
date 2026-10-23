// Package targetcheck defines the engine-free observation returned by a Panel
// candidate's fixed core-check command. It grants no installation authority.
package targetcheck

import coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"

const (
	MaxConfigBytes = 8 << 20
	MaxReportBytes = 64 << 10
	Schema         = "solovey.core-target-check/v1"
)

type Report struct {
	Schema              string                `json:"schema"`
	State               string                `json:"state"`
	CoreProvider        coreprovider.Identity `json:"coreProvider"`
	ConfigurationDigest string                `json:"configurationDigest,omitempty"`
	ReasonCodes         []string              `json:"reasonCodes"`
}
