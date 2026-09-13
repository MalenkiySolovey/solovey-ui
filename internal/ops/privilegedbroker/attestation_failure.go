package privilegedbroker

import (
	"errors"
	"fmt"
)

// PeerAttestationClass is a closed, payload-free diagnostic vocabulary. It is
// safe for broker audit events and deliberately carries no path, command line,
// credential, request, or environment value.
type PeerAttestationClass string

const (
	PeerAttestationCredentialsUnavailable PeerAttestationClass = "peer_credentials_unavailable"
	PeerAttestationUIDGIDMismatch         PeerAttestationClass = "peer_uid_gid_mismatch"
	PeerAttestationExecutableMismatch     PeerAttestationClass = "peer_executable_mismatch"
	PeerAttestationRoleNotAuthorized      PeerAttestationClass = "peer_role_not_authorized"
	PeerAttestationManifestAmbiguous      PeerAttestationClass = "peer_manifest_identity_ambiguous"
	PeerAttestationNamespaceMismatch      PeerAttestationClass = "peer_namespace_mismatch"
	PeerAttestationProcessMismatch        PeerAttestationClass = "peer_process_identity_mismatch"
	PeerAttestationStartIdentityMismatch  PeerAttestationClass = "peer_start_identity_mismatch"
	PeerAttestationBootMismatch           PeerAttestationClass = "peer_boot_identity_mismatch"
	PeerAttestationSupervisionMismatch    PeerAttestationClass = "peer_supervision_identity_mismatch"
	PeerAttestationCgroupPolicyMismatch   PeerAttestationClass = "peer_cgroup_policy_mismatch"
	PeerAttestationLivenessUnavailable    PeerAttestationClass = "peer_liveness_unavailable"
	PeerAttestationConnectorDeath         PeerAttestationClass = "peer_connector_death"
	PeerAttestationWriterMismatch         PeerAttestationClass = "peer_writer_mismatch"
	PeerAttestationGenerationMismatch     PeerAttestationClass = "peer_generation_mismatch"
	PeerAttestationInternalFailure        PeerAttestationClass = "peer_attestation_internal_failure"
)

type peerAttestationError struct {
	class PeerAttestationClass
	cause error
}

func (e *peerAttestationError) Error() string {
	if e == nil || e.cause == nil {
		return "broker peer attestation failed"
	}
	return fmt.Sprintf("broker peer attestation failed: %v", e.cause)
}

func (e *peerAttestationError) Unwrap() error { return e.cause }

func attestationFailure(class PeerAttestationClass, cause error) error {
	if cause == nil {
		cause = errors.New("peer proposition is invalid")
	}
	return &peerAttestationError{class: class, cause: cause}
}

func peerAttestationClass(err error) PeerAttestationClass {
	var failure *peerAttestationError
	if errors.As(err, &failure) && failure.class != "" {
		return failure.class
	}
	return PeerAttestationInternalFailure
}
