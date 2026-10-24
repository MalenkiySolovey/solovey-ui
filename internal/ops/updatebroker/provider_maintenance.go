package updatebroker

import (
	"encoding/json"
	"errors"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

// ProviderMaintenanceV1 freezes the existing Backup/Configuration/Maintenance
// owners' evidence before the hold. It names no filesystem object or command.
// The broker independently observes PreviousProvider in its authenticated peer.
type ProviderMaintenanceV1 struct {
	ConfigurationDigest        string                     `json:"configurationDigest"`
	ConfigurationBytes         int64                      `json:"configurationBytes"`
	ConfigurationRevision      string                     `json:"configurationRevision"`
	BackupRef                  string                     `json:"backupRef"`
	PreviousMaintenance        bool                       `json:"previousMaintenance"`
	PreviousProvider           release.CoreProviderSource `json:"previousProvider"`
	PreviousExecutableDigest   string                     `json:"previousExecutableDigest"`
	PreviousPanelVersion       string                     `json:"previousPanelVersion"`
	PreviousCapabilityRevision string                     `json:"previousCapabilityRevision"`
	RuntimeGeneration          string                     `json:"runtimeGeneration"`
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
	OperationID          string                     `json:"operationId"`
	ManifestDigest       string                     `json:"manifestDigest"`
	ArtifactSetDigest    string                     `json:"artifactSetDigest"`
	TargetSequence       uint64                     `json:"targetSequence"`
	TargetProvider       release.CoreProviderSource `json:"targetProvider"`
	TargetPanelVersion   string                     `json:"targetPanelVersion"`
	Input                ProviderMaintenanceV1      `json:"input"`
	Phase                string                     `json:"phase"`
	ExecutableDigest     string                     `json:"executableDigest,omitempty"`
	ReportDigest         string                     `json:"reportDigest,omitempty"`
	TargetCapabilities   string                     `json:"targetCapabilities,omitempty"`
	CapabilityRevision   string                     `json:"capabilityRevision,omitempty"`
	PreviousReportDigest string                     `json:"previousReportDigest,omitempty"`
	CapabilityDifference string                     `json:"capabilityDifference,omitempty"`
	Revision             string                     `json:"revision"`
}

func ValidateProviderMaintenanceObservation(value ProviderMaintenanceObservationV1) error {
	failed := errors.New("provider maintenance observation is invalid")
	if !safeOperationID(value.OperationID) || !validDigest(value.ManifestDigest) || !validDigest(value.ArtifactSetDigest) || value.TargetSequence == 0 ||
		release.ValidateCoreProviderSource(value.TargetProvider) != nil || !versionPattern.MatchString(value.TargetPanelVersion) ||
		ValidateProviderMaintenance(value.Input) != nil || !validDigest(value.Revision) {
		return failed
	}
	switch value.Phase {
	case "CHECKPOINTED":
		if !providerEvidenceEmpty(value) {
			return failed
		}
	case "VALIDATED", "ACTIVATED", "COMMITTED":
		if !providerEvidenceComplete(value) {
			return failed
		}
	case "ROLLED_BACK", "ROLLBACK_VERIFIED":
		// A rejected target can roll back before configuration validation. If
		// target evidence exists, it must remain complete and bound after restart.
		if !providerEvidenceEmpty(value) {
			if !providerEvidenceComplete(value) {
				return failed
			}
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

func providerEvidenceEmpty(value ProviderMaintenanceObservationV1) bool {
	return value.ExecutableDigest == "" && value.ReportDigest == "" && value.TargetCapabilities == "" && value.CapabilityRevision == "" &&
		value.PreviousReportDigest == "" && value.CapabilityDifference == ""
}

func providerEvidenceComplete(value ProviderMaintenanceObservationV1) bool {
	if !validDigest(value.ExecutableDigest) || !validDigest(value.ReportDigest) || !validDigest(value.PreviousReportDigest) ||
		validateProviderCapabilities(value.TargetCapabilities, value.CapabilityRevision, value.TargetProvider) != nil ||
		len(value.CapabilityDifference) == 0 || len(value.CapabilityDifference) > capabilityview.MaxDiffBytes {
		return false
	}
	var difference capabilityview.Difference
	if broker.DecodeRawPayload(json.RawMessage(value.CapabilityDifference), &difference) != nil ||
		difference.Validate(value.Input.PreviousCapabilityRevision, value.CapabilityRevision) != nil {
		return false
	}
	encoded, err := json.Marshal(difference)
	return err == nil && string(encoded) == value.CapabilityDifference
}

func ValidateProviderMaintenance(value ProviderMaintenanceV1) error {
	if !validDigest(value.ConfigurationDigest) || value.ConfigurationBytes <= 0 || value.ConfigurationBytes > targetcheck.MaxConfigBytes ||
		!validDigest(value.ConfigurationRevision) || !validDigest(value.BackupRef) || !validDigest(value.PreviousExecutableDigest) ||
		!validDigest(value.PreviousCapabilityRevision) || !versionPattern.MatchString(value.PreviousPanelVersion) ||
		release.ValidateCoreProviderSource(value.PreviousProvider) != nil || !validRuntimeGeneration(value.RuntimeGeneration) {
		return errors.New("provider maintenance checkpoint is invalid")
	}
	return nil
}

func validateProviderCapabilities(encoded, revision string, source release.CoreProviderSource) error {
	failed := errors.New("provider capability evidence is invalid")
	if len(encoded) == 0 || len(encoded) > capabilityview.MaxBytes || !validDigest(revision) {
		return failed
	}
	var value capabilityview.Snapshot
	if broker.DecodeRawPayload(json.RawMessage(encoded), &value) != nil || value.Validate(value.Provider, value.ComponentProfile) != nil ||
		!providerIdentityMatchesSource(value.Provider, source) || value.Revision() != revision {
		return failed
	}
	canonical, err := json.Marshal(value)
	if err != nil || string(canonical) != encoded {
		return failed
	}
	return nil
}

func providerIdentityMatchesSource(observed coreprovider.Identity, source release.CoreProviderSource) bool {
	if observed.Schema != coreprovider.Schema || observed.State != coreprovider.Verified || observed.Source != source ||
		observed.ArtifactState != coreprovider.Unknown || observed.ArtifactDigest != "" ||
		!validDigest(observed.Revision) || len(observed.ReasonCodes) != 1 || observed.ReasonCodes[0] != "ARTIFACT_DIGEST_NOT_OBSERVED" {
		return false
	}
	revision := observed.Revision
	observed.Revision = ""
	encoded, err := json.Marshal(observed)
	return err == nil && broker.Digest(encoded) == revision
}

func validRuntimeGeneration(value string) bool {
	return value == "" || len(value) <= 96 && safeIDPattern.MatchString(value)
}
