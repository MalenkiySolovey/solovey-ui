package hostsurface

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

type OwnerAvailability string

const (
	OwnerHelperNotInstalled        OwnerAvailability = "HELPER_NOT_INSTALLED"
	OwnerHelperIdentityMismatch    OwnerAvailability = "HELPER_IDENTITY_MISMATCH"
	OwnerHelperContractUnsupported OwnerAvailability = "HELPER_CONTRACT_UNSUPPORTED"
	OwnerOperationNotAdvertised    OwnerAvailability = "OPERATION_NOT_ADVERTISED"
	OwnerObserverNotRegistered     OwnerAvailability = "OWNER_OBSERVER_NOT_REGISTERED"
	OwnerObserverNotBound          OwnerAvailability = "OWNER_OBSERVER_NOT_BOUND"
	OwnerContractMismatch          OwnerAvailability = "OWNER_CONTRACT_MISMATCH"
	OwnerDeploymentBindingMismatch OwnerAvailability = "DEPLOYMENT_BINDING_MISMATCH"
	OwnerObservationTimeout        OwnerAvailability = "OBSERVATION_TIMEOUT"
	OwnerObservationFailed         OwnerAvailability = "OBSERVATION_FAILED"
	OwnerObservationStale          OwnerAvailability = "OBSERVATION_STALE"
	OwnerObservationAmbiguous      OwnerAvailability = "OBSERVATION_AMBIGUOUS"
	OwnerObservationSuccess        OwnerAvailability = "OBSERVATION_SUCCESS"
	OwnerHelperCapabilityStale     OwnerAvailability = "HELPER_CAPABILITY_STALE"
)

type OwnerObservation struct {
	Availability                  OwnerAvailability
	Observation                   *protectionhelper.ListenerOwnerObserveResult
	HelperIdentityRevision        string
	CapabilityRevision            string
	ListenerOwnerContractRevision string
	ListenerOwnerObserverRevision string
	ReasonCodes                   []string
}

type OwnerObserver interface {
	ObserveOwner(context.Context, hostresources.ProtectableResource) OwnerObservation
}

type helperExecutor interface {
	ExecuteWithMetadata(context.Context, protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error)
}

type HelperOwnerObserver struct {
	Helper helperExecutor
	Now    func() time.Time
}

type UnavailableOwnerObserver struct {
	Availability OwnerAvailability
}

func (o UnavailableOwnerObserver) ObserveOwner(context.Context, hostresources.ProtectableResource) OwnerObservation {
	availability := o.Availability
	if availability == "" {
		availability = OwnerObserverNotRegistered
	}
	return unavailableOwnerObservation(availability)
}

func (o HelperOwnerObserver) ObserveOwner(ctx context.Context, resource hostresources.ProtectableResource) OwnerObservation {
	if o.Helper == nil {
		return unavailableOwnerObservation(OwnerObserverNotRegistered)
	}
	expected := resource.Capabilities.ExpectedListenerOwner
	if !expected.Valid() {
		return unavailableOwnerObservation(OwnerContractMismatch, "listener_owner_expectation_missing")
	}
	if resource.ListenIntent.Schema != hostresources.ConfiguredListenIntentSchemaV1 {
		return unavailableOwnerObservation(OwnerObservationFailed, "listener_owner_listen_intent_invalid")
	}
	request := protectionhelper.Request{
		ProtocolVersion: protectionhelper.ProtocolVersion,
		Correlation:     protectionhelper.Correlation{OperationID: "listener-owner:" + hostresources.Revision(resource.ID + "|" + string(resource.ListenIntent.Network))[:24], InstanceID: expected.InstanceID},
		Operation:       protectionhelper.OperationListenerOwnerObserve,
		ListenerOwnerObserve: &protectionhelper.ListenerOwnerObserveRequest{
			ResourceID: resource.ID, Network: string(resource.ListenIntent.Network), ConfiguredMode: string(resource.ListenIntent.Mode),
			ConfiguredAddress: resource.ListenIntent.Address, Port: int(resource.ListenIntent.Port), ExpectedInstanceID: expected.InstanceID,
			ExpectedSourceRevision: expected.SourceRevision, ExpectedArtifactRevision: expected.ArtifactRevision,
			ExpectedDeploymentID: expected.DeploymentID, ExpectedOwnerContractRevision: expected.ContractRevision,
			ExpectedRuntimeRootBindingRevision: expected.RuntimeRootBindingRevision,
			ExpectedResourceOwnerRevision:      resource.Capabilities.OwnerRevision,
			ExpectedConfigurationRevision:      resource.Capabilities.ConfigRevision,
		},
	}
	response, metadata, err := o.Helper.ExecuteWithMetadata(ctx, request)
	base := OwnerObservation{
		HelperIdentityRevision: metadata.HelperIdentityRevision, CapabilityRevision: metadata.CapabilityRevision,
		ListenerOwnerContractRevision: metadata.ListenerOwnerContractRevision,
		ListenerOwnerObserverRevision: metadata.ListenerOwnerObserverRevision,
	}
	if !exactOwnerRevision(metadata.HelperIdentityRevision) {
		return ownerObservationFailure(base, OwnerHelperIdentityMismatch)
	}
	if err != nil || !response.OK || response.ListenerOwner == nil {
		return ownerObservationFailure(base, ownerExecutionFailure(ctx, response, err), boundedOwnerReason(response.Reason))
	}
	if !exactOwnerRevision(metadata.CapabilityRevision) || !exactOwnerRevision(metadata.ListenerOwnerObserverRevision) {
		return ownerObservationFailure(base, OwnerHelperContractUnsupported)
	}
	if metadata.ListenerOwnerContractRevision != expected.ContractRevision {
		return ownerObservationFailure(base, OwnerContractMismatch)
	}

	result := *response.ListenerOwner
	result.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), response.ListenerOwner.Facts...)
	result.ReasonCodes = append([]string(nil), response.ListenerOwner.ReasonCodes...)
	if !protectionhelper.ListenerOwnerResultValid(&result) {
		return ownerObservationFailure(base, OwnerObservationFailed, "listener_owner_revision_invalid")
	}
	availability := availabilityForOwnerReasons(result.ReasonCodes)
	if availability != OwnerObservationSuccess {
		return ownerObservationFailure(base, availability, result.ReasonCodes...)
	}

	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	for _, fact := range result.Facts {
		availability, reason := validateOwnerFact(fact, resource, now)
		if availability != OwnerObservationSuccess {
			return ownerObservationFailure(base, availability, append(result.ReasonCodes, reason)...)
		}
	}
	if len(result.Facts) == 0 && !slices.Contains(result.ReasonCodes, "listener_unobserved") {
		return ownerObservationFailure(base, OwnerObservationFailed, "listener_owner_fact_missing")
	}
	base.Availability = OwnerObservationSuccess
	base.Observation = &result
	base.ReasonCodes = normalizedOwnerStateReasons(result.ReasonCodes)
	return base
}

func ownerExecutionFailure(ctx context.Context, response protectionhelper.Response, err error) OwnerAvailability {
	switch {
	case errors.Is(err, protectionhelper.ErrHelperIdentityMismatch) || response.Reason == "helper_identity_mismatch":
		return OwnerHelperIdentityMismatch
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || response.Code == protectionhelper.CodeTimeout || response.Reason == "timeout":
		return OwnerObservationTimeout
	case response.Reason == "helper_version_mismatch":
		return OwnerHelperContractUnsupported
	case response.Reason == "listener_owner_contract_unavailable":
		return OwnerContractMismatch
	case response.Code == protectionhelper.CodeMissingCapability || response.Reason == "missing_capability":
		return OwnerOperationNotAdvertised
	default:
		return OwnerObservationFailed
	}
}

func availabilityForOwnerReasons(reasons []string) OwnerAvailability {
	for _, reason := range reasons {
		switch reason {
		case "listener_owner_contract_unavailable":
			return OwnerContractMismatch
		case "listener_deployment_mismatch":
			return OwnerDeploymentBindingMismatch
		case "listener_owner_stale":
			return OwnerObservationStale
		case "listener_owner_ambiguous":
			return OwnerObservationAmbiguous
		case "listener_owner_capability_unavailable", "listener_owner_systemd_unavailable", "listener_owner_unavailable", "listener_owner_scan_bounded", "listener_service_unavailable", "listener_process_identity_mismatch":
			return OwnerObservationFailed
		}
	}
	return OwnerObservationSuccess
}

func validateOwnerFact(fact hostfacts.ListenerOwnerFactV1, resource hostresources.ProtectableResource, now time.Time) (OwnerAvailability, string) {
	expected, application := resource.Capabilities.ExpectedListenerOwner, fact.Application
	if fact.ExpiresAt <= now.Unix() {
		return OwnerObservationStale, "listener_owner_stale"
	}
	if application.InstanceID != expected.InstanceID || application.SourceRevision != expected.SourceRevision ||
		application.ArtifactRevision != expected.ArtifactRevision || application.DeploymentID != expected.DeploymentID ||
		application.ResourceID != resource.ID || application.ResourceOwnerRevision != resource.Capabilities.OwnerRevision ||
		application.ConfigurationRevision != resource.Capabilities.ConfigRevision {
		return OwnerDeploymentBindingMismatch, "listener_deployment_mismatch"
	}
	if application.OwnerContractRevision != expected.ContractRevision || application.RuntimeRootBindingRevision != expected.RuntimeRootBindingRevision ||
		application.ExpectedExecutableSHA256 != expected.ExecutableSHA256 || application.ServiceIdentity != expected.ServiceIdentity ||
		fact.Service.SystemdUnit != expected.SystemdUnit || fact.Service.FragmentPath != expected.ServiceFragmentPath ||
		fact.Service.FragmentSHA256 != expected.ServiceUnitSHA256 || fact.Service.ControlGroup != expected.ServiceControlGroup ||
		fact.Process.ControlGroup != expected.ServiceControlGroup || fact.Process.Executable != expected.ExecutablePath ||
		fact.Process.UID == nil || fact.Process.GID == nil || uint32(*fact.Process.UID) != expected.ProcessUID || uint32(*fact.Process.GID) != expected.ProcessGID {
		return OwnerContractMismatch, "listener_owner_contract_mismatch"
	}
	if !fact.Valid(now) {
		return OwnerObservationFailed, "listener_owner_fact_invalid"
	}
	return OwnerObservationSuccess, ""
}

func unavailableOwnerObservation(availability OwnerAvailability, reasons ...string) OwnerObservation {
	return ownerObservationFailure(OwnerObservation{}, availability, reasons...)
}

func ownerObservationFailure(base OwnerObservation, availability OwnerAvailability, reasons ...string) OwnerObservation {
	base.Availability = availability
	base.Observation = nil
	reasons = append(reasons, ownerAvailabilityReason(availability))
	switch availability {
	case OwnerObservationStale:
		reasons = append(reasons, "listener_owner_stale")
	case OwnerObservationAmbiguous:
		reasons = append(reasons, "listener_owner_ambiguous")
	case OwnerDeploymentBindingMismatch:
		reasons = append(reasons, "listener_deployment_mismatch")
	default:
		reasons = append(reasons, "listener_owner_capability_unavailable")
	}
	base.ReasonCodes = normalizedOwnerStateReasons(reasons)
	return base
}

func ownerAvailabilityReason(availability OwnerAvailability) string {
	switch availability {
	case OwnerHelperNotInstalled:
		return "listener_owner_helper_not_installed"
	case OwnerHelperIdentityMismatch:
		return "listener_owner_helper_identity_mismatch"
	case OwnerHelperContractUnsupported:
		return "listener_owner_helper_contract_unsupported"
	case OwnerOperationNotAdvertised:
		return "listener_owner_operation_not_advertised"
	case OwnerObserverNotRegistered:
		return "listener_owner_observer_not_registered"
	case OwnerObserverNotBound:
		return "listener_owner_observer_not_bound"
	case OwnerContractMismatch:
		return "listener_owner_contract_mismatch"
	case OwnerDeploymentBindingMismatch:
		return "listener_owner_deployment_binding_mismatch"
	case OwnerHelperCapabilityStale:
		return "listener_owner_helper_capability_stale"
	default:
		return "listener_owner_" + strings.ToLower(string(availability))
	}
}

func boundedOwnerReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" || len(reason) > 96 {
		return ""
	}
	for _, value := range reason {
		if value != '_' && value != '-' && (value < 'a' || value > 'z') && (value < '0' || value > '9') {
			return "listener_owner_failure_reason_invalid"
		}
	}
	return reason
}

func normalizedOwnerStateReasons(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = boundedOwnerReason(value); value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	slices.Sort(result)
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

func exactOwnerRevision(value string) bool {
	return len(value) == 64 && value == strings.ToLower(value) && func() bool { _, err := hex.DecodeString(value); return err == nil }()
}
