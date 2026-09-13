package helper

import (
	"context"
	"errors"

	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

// SSHRecoveryExecutor is the helper-protocol projection of the SSH owner's
// bounded recovery observer. It deliberately has no daemon, configuration,
// service-control, executable, or log-backend authority.
type SSHRecoveryExecutor interface {
	Detect(context.Context) SSHRecoverySupport
	Observe(context.Context, SSHRecoveryObserveRequest) (*SSHRecoveryResult, error)
}

type sshRecoveryOwnerAdapter struct {
	owner sshbroker.RecoveryObserver
}

func newComposedSSHRecoveryExecutor(composition sshbroker.ResolvedSSHComposition) SSHRecoveryExecutor {
	return newSSHRecoveryExecutorFromOwner(sshbroker.NewRecoveryObserver(composition))
}

func newSSHRecoveryExecutorFromOwner(owner sshbroker.RecoveryObserver) SSHRecoveryExecutor {
	return sshRecoveryOwnerAdapter{owner: owner}
}

func (a sshRecoveryOwnerAdapter) Detect(ctx context.Context) SSHRecoverySupport {
	if a.owner == nil {
		return SSHRecoverySupport{Reason: "ssh_recovery_owner_unavailable", ObserverRevision: sshbroker.RecoveryObserverRevision()}
	}
	return projectSSHRecoverySupport(a.owner.Detect(ctx))
}

func (a sshRecoveryOwnerAdapter) Observe(ctx context.Context, request SSHRecoveryObserveRequest) (*SSHRecoveryResult, error) {
	if a.owner == nil {
		return nil, errors.New("ssh_recovery_owner_unavailable")
	}
	result, err := a.owner.Observe(ctx, sshbroker.RecoveryObserveRequest{SinceUnixMicros: request.SinceUnixMicros, MaxEvents: request.MaxEvents})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("ssh_recovery_owner_returned_no_result")
	}
	observations := make([]SSHRecoveryObservation, len(result.Observations))
	for index, observation := range result.Observations {
		observations[index] = SSHRecoveryObservation{ObservationID: observation.ObservationID, PrincipalID: observation.PrincipalID,
			SourcePrefix: observation.SourcePrefix, AuthenticationClass: observation.AuthenticationClass,
			ObservedAt: observation.ObservedAt, ObservedAtMicros: observation.ObservedAtMicros}
	}
	return &SSHRecoveryResult{VerifierRevision: result.VerifierRevision, ObserverRevision: result.ObserverRevision, Observations: observations}, nil
}

func projectSSHRecoverySupport(value sshbroker.RecoverySupport) SSHRecoverySupport {
	return SSHRecoverySupport{PlatformKnown: value.PlatformKnown, Linux: value.Linux, Available: value.Available,
		Reason: value.Reason, EvidenceKind: SSHRecoveryEvidenceKind(value.EvidenceKind), VerifierRevision: value.VerifierRevision,
		ObserverRevision: value.ObserverRevision}
}
