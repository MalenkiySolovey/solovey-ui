package updatebroker

import (
	"encoding/json"
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

// ProviderMaintenanceV1 freezes the existing Backup/Configuration/Maintenance
// owners' evidence before the hold. It names no filesystem object or command.
// The broker independently observes PreviousProvider in its authenticated peer.
type ProviderMaintenanceV1 struct {
	ConfigurationDigest      string                     `json:"configurationDigest"`
	ConfigurationBytes       int64                      `json:"configurationBytes"`
	ConfigurationRevision    string                     `json:"configurationRevision"`
	BackupRef                string                     `json:"backupRef"`
	PreviousMaintenance      bool                       `json:"previousMaintenance"`
	PreviousProvider         release.CoreProviderSource `json:"previousProvider"`
	PreviousExecutableDigest string                     `json:"previousExecutableDigest"`
	RuntimeGeneration        string                     `json:"runtimeGeneration"`
}

// ProviderHealthV1 is the authenticated Panel's existing runtime/health owner's
// post-restart observation. It cannot replace the broker's executable binding.
type ProviderHealthV1 struct {
	ConfigurationDigest   string `json:"configurationDigest"`
	ConfigurationRevision string `json:"configurationRevision"`
	Maintenance           bool   `json:"maintenance"`
	RuntimeState          string `json:"runtimeState"`
	RuntimeGeneration     string `json:"runtimeGeneration"`
	CapabilityRevision    string `json:"capabilityRevision"`
}

// The existing Update owner recovers its checkpoint through this authenticated
// observation after restart; no provider-only database schema is required.
type ProviderMaintenanceObservationV1 struct {
	OperationID       string                     `json:"operationId"`
	ManifestDigest    string                     `json:"manifestDigest"`
	ArtifactSetDigest string                     `json:"artifactSetDigest"`
	TargetSequence    uint64                     `json:"targetSequence"`
	TargetProvider    release.CoreProviderSource `json:"targetProvider"`
	Input             ProviderMaintenanceV1      `json:"input"`
	Phase             string                     `json:"phase"`
	ExecutableDigest  string                     `json:"executableDigest,omitempty"`
	ReportDigest      string                     `json:"reportDigest,omitempty"`
	Revision          string                     `json:"revision"`
}

func ValidateProviderMaintenanceObservation(value ProviderMaintenanceObservationV1) error {
	failed := errors.New("provider maintenance observation is invalid")
	if !safeOperationID(value.OperationID) || !validDigest(value.ManifestDigest) || !validDigest(value.ArtifactSetDigest) || value.TargetSequence == 0 ||
		release.ValidateCoreProviderSource(value.TargetProvider) != nil || ValidateProviderMaintenance(value.Input) != nil || !validDigest(value.Revision) {
		return failed
	}
	switch value.Phase {
	case "CHECKPOINTED":
		if value.ExecutableDigest != "" || value.ReportDigest != "" {
			return failed
		}
	case "VALIDATED", "ACTIVATED", "COMMITTED":
		if !validDigest(value.ExecutableDigest) || !validDigest(value.ReportDigest) {
			return failed
		}
	default:
		return failed
	}
	revision := value.Revision
	value.Revision = ""
	encoded, err := json.Marshal(value)
	if err != nil || broker.Digest(encoded) != revision {
		return failed
	}
	return nil
}

func ValidateProviderMaintenance(value ProviderMaintenanceV1) error {
	if !validDigest(value.ConfigurationDigest) || value.ConfigurationBytes <= 0 || value.ConfigurationBytes > targetcheck.MaxConfigBytes ||
		!validDigest(value.ConfigurationRevision) || !validDigest(value.BackupRef) || !validDigest(value.PreviousExecutableDigest) ||
		release.ValidateCoreProviderSource(value.PreviousProvider) != nil || !validRuntimeGeneration(value.RuntimeGeneration) {
		return errors.New("provider maintenance checkpoint is invalid")
	}
	return nil
}

func validRuntimeGeneration(value string) bool {
	return value == "" || len(value) <= 96 && safeIDPattern.MatchString(value)
}
