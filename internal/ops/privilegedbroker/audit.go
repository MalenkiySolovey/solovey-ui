package privilegedbroker

func sanitizeAuditVerb(verb Verb) Verb {
	switch verb {
	case VerbCapabilities,
		VerbSSHObserve, VerbSSHPrepare, VerbSSHStage, VerbSSHRecoverStage, VerbSSHReleaseStage, VerbSSHValidate, VerbSSHReload, VerbSSHArm, VerbSSHRestore, VerbSSHInspect, VerbSSHVerify, VerbSSHProof,
		VerbDeploymentObserve, VerbDeploymentDoctor, VerbDeploymentPrepare, VerbDeploymentRelease, VerbDeploymentApply, VerbDeploymentVerify, VerbDeploymentRollback,
		VerbUpdateObserve, VerbUpdateStage, VerbUpdateRelease, VerbUpdatePrepare, VerbUpdateActivate, VerbUpdateVerify, VerbUpdateRollback:
		return verb
	case "unknown", "invalid":
		return verb
	default:
		if validVerb(string(verb)) {
			return "unknown"
		}
		return "invalid"
	}
}

func sanitizeAuditOperation(value string) string {
	switch value {
	case "", "absent":
		return "absent"
	case "present", "invalid":
		return value
	default:
		if safeIdentifier(value) {
			return "present"
		}
		return "invalid"
	}
}

func sanitizeAuditDigest(value string) string {
	if digestPattern.MatchString(value) {
		return value
	}
	return ""
}

func sanitizePeerAttestation(value PeerAttestationClass) PeerAttestationClass {
	switch value {
	case "",
		PeerAttestationCredentialsUnavailable, PeerAttestationUIDGIDMismatch, PeerAttestationExecutableMismatch,
		PeerAttestationRoleNotAuthorized, PeerAttestationManifestAmbiguous, PeerAttestationNamespaceMismatch,
		PeerAttestationProcessMismatch, PeerAttestationStartIdentityMismatch, PeerAttestationBootMismatch,
		PeerAttestationSupervisionMismatch, PeerAttestationCgroupPolicyMismatch, PeerAttestationLivenessUnavailable,
		PeerAttestationConnectorDeath, PeerAttestationWriterMismatch, PeerAttestationGenerationMismatch,
		PeerAttestationInternalFailure:
		return value
	default:
		return PeerAttestationInternalFailure
	}
}

func sanitizeAuditPhase(value AuditPhase) AuditPhase {
	switch value {
	case AuditPhaseInitialAttestation, AuditPhaseRequestReceive, AuditPhaseFinalRecheck, AuditPhaseDispatchGate:
		return value
	default:
		return AuditPhaseDispatchGate
	}
}

func sanitizeAuditResult(value string) string {
	switch value {
	case "success", "replay", "denied_peer", "audit_queue_saturated",
		"denied_invalid_request", "denied_unauthorized_peer", "denied_unsupported_verb",
		"denied_capability_unavailable", "denied_deadline_exceeded", "denied_idempotency_conflict",
		"denied_stale_fence", "denied_revision_mismatch", "denied_recovery_required",
		"denied_validation_failed", "denied_execution_failed", "denied_internal_error":
		return value
	default:
		return "denied_internal_error"
	}
}

func sanitizeAuditDuration(value string) string {
	switch value {
	case "lt_10ms", "lt_100ms", "lt_1s", "lt_10s", "gte_10s", "not_measured":
		return value
	default:
		return "not_measured"
	}
}

func sanitizeRevisionTransition(value string) string {
	if value == "expected_revision_present" {
		return value
	}
	return "none"
}

func sanitizeRecoveryClass(value string) string {
	if value == "manual_recovery_required" {
		return value
	}
	return "none"
}
