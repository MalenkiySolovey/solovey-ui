// Package deploymentadapter projects installed deployment proofs into the
// bounded concrete facts needed by Server Protection composition.
package deploymentadapter

import (
	"errors"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
)

var ErrInstalledRecoveryProjectionUnavailable = errors.New("installed recovery authority projection is unavailable")

type RecoveryAuthority string

const (
	RecoveryAuthorityAuthenticatedBroker RecoveryAuthority = "authenticated_broker"
	RecoveryAuthorityExternalOperator    RecoveryAuthority = "external_operator"
)

// RecoveryAction is bounded operator guidance only. It is never executed by
// the panel or privileged broker.
type RecoveryAction struct {
	Program string
	Args    []string
	Purpose string
}

// RecoveryProjection records who owns the first recovery action and whether
// Server Protection has authenticated self-recovery authority. It is a
// component-local deployment fact, not a platform or supervisor taxonomy.
type RecoveryProjection struct {
	Authority             RecoveryAuthority
	SelfRecoveryAvailable bool
	Action                RecoveryAction
	DeploymentBackend     string
	ProjectionRevision    string
	OwnerContractRevision string
}

func systemdBrokerRecoveryProjection() RecoveryProjection {
	return RecoveryProjection{
		Authority: RecoveryAuthorityAuthenticatedBroker, SelfRecoveryAvailable: true,
		Action: RecoveryAction{Program: "systemctl", Args: []string{"is-active", "solovey-privileged-broker.socket"},
			Purpose: "verify_privileged_broker_socket"},
	}
}

func procdBrokerRecoveryProjection() RecoveryProjection {
	return RecoveryProjection{
		Authority: RecoveryAuthorityAuthenticatedBroker, SelfRecoveryAvailable: true,
		Action: RecoveryAction{Program: "ubus", Args: []string{"call", "service", "list", `{"name":"solovey-ui","instance":"root-broker"}`},
			Purpose: "verify_privileged_broker_instance"},
	}
}

// dockerOperatorRecoveryProjection records the only recovery authority available
// to the unprivileged Docker profile. Docker deliberately has no broker
// socket or daemon control channel, so recovery is an operator-managed
// container recreation rather than a fabricated systemd/procd readiness
// command. SelfRecoveryAvailable remains false: the action is guidance in a
// recovery bundle and is never executed by the panel or privileged broker.
func dockerOperatorRecoveryProjection() RecoveryProjection {
	return RecoveryProjection{
		Authority: RecoveryAuthorityExternalOperator, SelfRecoveryAvailable: false,
		Action: RecoveryAction{Program: "operator_review", Args: []string{"container_replacement_or_restart"},
			Purpose: "external_operator_managed_recovery"},
	}
}

// RecoveryProjectionForRuntimeAuthority derives recovery guidance from the
// already selected and fenced runtime-root projection. It performs no backend
// discovery and cannot mix guidance from a later Systemd/procd observation.
func RecoveryProjectionForRuntimeAuthority(authority protectionruntime.RuntimeRootAuthority) (RecoveryProjection, error) {
	if err := authority.Validate(); err != nil || !authority.Installed() {
		return RecoveryProjection{}, ErrInstalledRecoveryProjectionUnavailable
	}
	projection, err := recoveryProjectionForBackend(authority.Backend())
	if err != nil {
		return RecoveryProjection{}, err
	}
	projection.DeploymentBackend = string(authority.Backend())
	projection.ProjectionRevision = authority.ProjectionRevision()
	projection.OwnerContractRevision = authority.OwnerContractRevision()
	return projection, nil
}

func recoveryProjectionForBackend(backend protectionruntime.DeploymentBackend) (RecoveryProjection, error) {
	switch backend {
	case protectionruntime.DeploymentBackendSystemd:
		return systemdBrokerRecoveryProjection(), nil
	case protectionruntime.DeploymentBackendProcd:
		return procdBrokerRecoveryProjection(), nil
	case protectionruntime.DeploymentBackendDocker:
		return dockerOperatorRecoveryProjection(), nil
	default:
		return RecoveryProjection{}, errors.New("installed recovery authority backend is unavailable")
	}
}
