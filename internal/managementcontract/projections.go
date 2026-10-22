package managementcontract

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func representable(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return wire.ValidateDocument(name, data)
}

// ProjectConfiguredResource exports the existing configured identity only.
// A missing/foreign identity does not imply Local or an observed healthy state.
func ProjectConfiguredResource(value resources.ProtectableResource) (wire.ResourceRef, wire.Revision, error) {
	if value.Identity == nil || !value.Identity.ValidFor(value) {
		return wire.ResourceRef{}, "", errors.New("STALE_STATE")
	}
	identity := value.Identity
	result := wire.ResourceRef{Host: LocalHost(), Owner: identity.Owner, ResourceID: identity.ResourceID}
	if err := representable("ResourceRef", result); err != nil {
		return wire.ResourceRef{}, "", err
	}
	revision := wire.Revision(identity.DesiredRevision)
	return result, revision, representable("Revision", revision)
}

func operationReference(value operationcoordination.Reference) (wire.OperationRef, error) {
	if !value.HostRef.IsLocal() {
		return wire.OperationRef{}, errors.New("UNSUPPORTED")
	}
	result := wire.OperationRef{Host: LocalHost(), Owner: value.Owner, ResourceID: value.ResourceID,
		OperationID: value.OperationID, CorrelationID: value.CorrelationID, ChangeID: value.ChangeID}
	return result, representable("OperationRef", result)
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func revision(value string) *wire.Revision {
	if value == "" {
		return nil
	}
	result := wire.Revision(value)
	return &result
}

// Owner diagnostics remain supplementary codes, never a new success class.
func problem(value *operationcoordination.Problem) *wire.Problem {
	if value == nil {
		return nil
	}
	code := wire.ReasonCode("INTERNAL")
	switch value.Code {
	case "update_revision_mismatch":
		code = "REVISION_CONFLICT"
	case "update_operation_conflict":
		code = "RESOURCE_CONFLICT"
	case "verified_release_changed":
		code = "STALE_STATE"
	case "release_signing_unavailable":
		code = "AUTHORITY_UNAVAILABLE"
	case "update_provider_unavailable":
		code = "CAPABILITY_UNAVAILABLE"
	case "resource_pressure_blocks_update":
		code = "DEPENDENCY_UNAVAILABLE"
	case "update_operator_managed", "update_package_managed":
		code = "UNSUPPORTED"
	}
	if value.RecoveryRequired {
		code = "RECOVERY_REQUIRED"
	}
	return &wire.Problem{Owner: value.Owner, ResourceID: value.ResourceID, Phase: value.Phase,
		Code: code, OwnerReason: optional(value.Code), RecoveryRequired: value.RecoveryRequired}
}

func ProjectChange(value operationcoordination.Change) (wire.Change, error) {
	reference, err := operationReference(value.Reference)
	if err != nil {
		return wire.Change{}, err
	}
	result := wire.Change{Reference: reference, Revision: wire.Generation(strconv.FormatUint(value.Revision, 10)),
		ExpectedRevision: revision(value.ExpectedRevision), Phase: value.Phase, OwnerPhase: value.OwnerPhase,
		Outcome: wire.Outcome(value.Outcome), CheckpointRef: optional(value.CheckpointRef),
		RollbackVerified: value.RollbackVerified, RecoveryRequired: value.RecoveryRequired, Problem: problem(value.Problem)}
	return result, representable("Change", result)
}

func ProjectJob(value operationcoordination.Job) (wire.Job, error) {
	reference, err := operationReference(value.Reference)
	if err != nil {
		return wire.Job{}, err
	}
	result := wire.Job{Reference: reference, JobID: value.JobID, Revision: wire.Generation(strconv.FormatUint(value.Revision, 10)),
		Phase: value.Phase, ExpectedRevision: revision(value.ExpectedRevision),
		CancelSupported: value.CancelSupported, CriticalSection: value.CriticalSection, Outcome: wire.Outcome(value.Outcome),
		RecoveryRequired: value.RecoveryRequired, Problem: problem(value.Problem), ResultRef: optional(value.ResultRef)}
	if value.Progress != nil {
		result.Progress = &wire.Progress{Completed: value.Progress.Completed, Total: value.Progress.Total, Unit: value.Progress.Unit}
	}
	if value.CancelRequested != nil {
		requested := *value.CancelRequested
		result.CancelRequested = &requested
	}
	if value.Deadline != 0 {
		deadline := value.Deadline
		result.Deadline = &deadline
	}
	return result, representable("Job", result)
}

// ProjectHealth calls the existing proposition/freshness decision. Receiving or
// serializing an observation never refreshes it. It invents no runtime epoch.
func ProjectHealth(value health.Result, requirement health.Requirement, now time.Time) (wire.Health, error) {
	result := wire.Health{Resource: wire.ResourceRef{Host: LocalHost(), Owner: requirement.Owner, ResourceID: value.ResourceID},
		Status: "UNKNOWN", Reason: "STALE_STATE"}
	if e := value.Evidence; e != nil {
		projected := wire.HealthEvidence{Resource: wire.ResourceRef{Host: wire.HostRef{Kind: e.HostRef.Kind, ID: e.HostRef.ID}, Owner: e.Owner, ResourceID: e.ResourceID},
			ProofKind: wire.ProofKind(e.ProofKind), Source: e.Source, DesiredRevision: revision(e.DesiredRevision),
			ObservedRevision: wire.Revision(e.ObservedRevision), EvidenceRevision: wire.Digest(e.EvidenceRevision),
			ObservedAt: e.ObservedAt, MaxAgeSeconds: e.MaxAgeSeconds, EvidenceRef: e.EvidenceRef}
		if e.HostRef.IsLocal() && projected.Resource == result.Resource && representable("HealthEvidence", projected) == nil {
			result.Evidence = &projected
		}
	}
	switch value.Status {
	case health.StatusOK:
		if result.Evidence != nil && value.Satisfies(requirement, now) {
			result.Status, result.Reason = "PASS", "ACCEPTED"
		}
	case health.StatusDegraded:
		result.Reason = "DEPENDENCY_UNAVAILABLE"
		// Unavailable proof is not a proven feature failure at this revision.
		proof := value
		proof.Status = health.StatusOK
		if result.Evidence != nil && value.FactCode != "" && value.FactCode != "health_evidence_unsatisfied" &&
			!strings.HasPrefix(value.FactCode, "health_check_") && proof.Satisfies(requirement, now) {
			result.Status = "FAIL"
		}
	case health.StatusMissingCapability:
		result.Reason = "CAPABILITY_UNAVAILABLE"
	}
	return result, representable("Health", result)
}

// ProjectComponent uses installed/enabled/binary facts and explicit owner Health.
// Active and a successful Start do not imply a healthy feature.
func ProjectComponent(m manifest.Manifest, value state.Component, check *health.Result, requirement health.Requirement, resourceID string, now time.Time) (wire.Component, error) {
	if m.ID != value.ID {
		return wire.Component{}, errors.New("INVALID_INPUT")
	}
	result := wire.Component{Host: LocalHost(), Manifest: wire.ComponentManifest{ComponentID: m.ID, Version: m.Version, Actions: append([]string{}, m.Actions...)},
		State: "UNKNOWN", Reason: "UNKNOWN", LifecyclePhase: optional(value.ObservedLifecycle.Phase), InstanceRetention: optional(value.ObservedLifecycle.InstanceRetention)}
	switch {
	case !value.Installed:
		result.State, result.Reason = "ABSENT", "ABSENT"
	case !value.Enabled:
		result.State, result.Reason = "INSTALLED_DISABLED", "DISABLED"
	case !value.AvailableInBinary:
		result.State, result.Reason = "ENABLED_UNSUPPORTED", "UNSUPPORTED"
	case check != nil && requirement.Owner == m.ID && check.ResourceID == resourceID:
		proof, err := ProjectHealth(*check, requirement, now)
		if err != nil {
			return wire.Component{}, err
		}
		if proof.Status == "PASS" {
			result.State, result.Reason, result.EvidenceRef = "ENABLED_HEALTHY", "ACCEPTED", optional(proof.Evidence.EvidenceRef)
		}
		if proof.Status == "FAIL" {
			result.State, result.Reason = "ENABLED_UNHEALTHY", proof.Reason
		}
	}
	return result, representable("Component", result)
}

func ProjectCapability(owner, id, ownerRevision, source string, known bool, value resources.CapabilityValue) (wire.Capability, error) {
	if !known {
		value = resources.CapabilityUnknown
	}
	result := wire.Capability{Host: LocalHost(), Owner: owner, ID: id, Value: string(value), Revision: wire.Revision(ownerRevision), Source: source}
	return result, representable("Capability", result)
}

// Verified metadata cannot be manufactured from JSON or a checksum. Admission
// still requires the existing release verifier's unexported signature binding.
func ProjectArtifact(verified release.Verified, artifact release.Artifact) (wire.ArtifactTrust, error) {
	identity, err := verified.ArtifactIdentity(artifact)
	if err != nil {
		return wire.ArtifactTrust{}, err
	}
	result := wire.ArtifactTrust{Status: "VERIFIED", Reason: "ACCEPTED", Artifact: wire.ArtifactIdentity{
		ArtifactID: identity.ArtifactID, Owner: identity.Owner, Kind: identity.Kind, Version: identity.Version,
		Platform: identity.Platform, Arch: identity.Arch, ContentDigest: wire.Digest(identity.ContentDigest), Size: identity.Size,
		SignerRef: identity.SignerRef, ProvenanceRef: identity.ProvenanceRef, SourceRevision: wire.Digest(identity.SourceRevision)}}
	return result, representable("ArtifactTrust", result)
}
