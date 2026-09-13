package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type SystemdBrokerProvider struct{ client *broker.Client }

func init() {
	factory := func() Provider { return NewSystemdBrokerProvider(nil) }
	// "native" is a released environment-value compatibility reader. Current
	// Systemd deployment writers emit the explicit "systemd" value.
	registerRuntimeProvider("native", factory)
	registerRuntimeProvider("systemd", factory)
}

func NewSystemdBrokerProvider(client *broker.Client) *SystemdBrokerProvider {
	if client == nil {
		client = broker.NewClient(broker.RolePanel)
	}
	return &SystemdBrokerProvider{client: client}
}

func (*SystemdBrokerProvider) ProviderID() string { return domain.ProviderV1 }
func (*SystemdBrokerProvider) UpdateLifecycle() UpdateLifecycle {
	return UpdateLifecycleSelfManaged
}

func (p *SystemdBrokerProvider) BrokerPresentation(ctx context.Context) BrokerPresentation {
	return BrokerPresentation{Available: p.Capabilities(ctx).Observe == domain.Available, ProtocolRevision: domain.ProviderV1,
		Transport: "systemd-activated-unix-peer-credentials", PeerPosture: "root-owned-pinned-client-manifest"}
}

func (p *SystemdBrokerProvider) Capabilities(ctx context.Context) domain.Capabilities {
	result := domain.Capabilities{Observe: domain.Unavailable, Doctor: domain.Unavailable, Migrate: domain.Unavailable,
		Rollback: domain.Unavailable, Reasons: []string{"privileged_broker_unavailable"}}
	if p != nil && p.client != nil && runtime.GOOS == "linux" {
		var capabilities broker.CapabilitiesV1
		_, err := p.client.Invoke(ctx, broker.Call{Verb: broker.VerbCapabilities, OperationID: "deployment-capabilities",
			Timeout: 3 * time.Second, Payload: contract.EmptyV1{}}, &capabilities)
		if err == nil && containsVerbs(capabilities.Verbs, broker.VerbDeploymentObserve, broker.VerbDeploymentPrepare,
			broker.VerbDeploymentRelease, broker.VerbDeploymentDoctor, broker.VerbDeploymentApply, broker.VerbDeploymentVerify, broker.VerbDeploymentRollback) && len(capabilities.Unresolved) == 0 {
			if report, doctorErr := p.Doctor(ctx); doctorErr == nil {
				result = report.Capabilities
			}
		}
	}
	result.Revision = ""
	result.Revision = domain.Revision(result)
	return result
}

func (p *SystemdBrokerProvider) Observe(ctx context.Context) (domain.Posture, error) {
	var result contract.ObservationV1
	if err := p.read(ctx, broker.VerbDeploymentObserve, "deployment-observe", contract.EmptyV1{}, &result); err != nil {
		return domain.Posture{}, err
	}
	if result.ProviderRevision != contract.ProviderRevision {
		return domain.Posture{}, ErrRevisionMismatch
	}
	return result.Posture, nil
}

func (p *SystemdBrokerProvider) Doctor(ctx context.Context) (domain.DoctorReport, error) {
	var result contract.DoctorResultV1
	if err := p.read(ctx, broker.VerbDeploymentDoctor, "deployment-doctor", contract.EmptyV1{}, &result); err != nil {
		return domain.DoctorReport{}, err
	}
	if result.ProviderRevision != contract.ProviderRevision {
		return domain.DoctorReport{}, ErrRevisionMismatch
	}
	return result.Report, nil
}

func (p *SystemdBrokerProvider) Prepare(ctx context.Context, fence FenceV1, target domain.ProfileID) (string, error) {
	var result contract.PrepareResultV1
	if err := p.mutate(ctx, broker.VerbDeploymentPrepare, fence, 1, contract.PrepareRequestV1{TargetProfile: target}, &result); err != nil {
		return "", err
	}
	if result.ProviderRevision != contract.ProviderRevision || len(result.CheckpointRef) != 64 {
		return "", ErrRevisionMismatch
	}
	return result.CheckpointRef, nil
}

func (p *SystemdBrokerProvider) RecoverPreparedCheckpoint(ctx context.Context, fence FenceV1, target domain.ProfileID) (string, error) {
	// Prepare is an exact broker-journal replay: the deterministic request and
	// idempotency identity are identical to the original call, so the handler is
	// not executed a second time.
	return p.Prepare(ctx, fence, target)
}

func (p *SystemdBrokerProvider) ReleaseCheckpoint(ctx context.Context, fence FenceV1, checkpoint string) error {
	if len(checkpoint) != 64 {
		return ErrRevisionMismatch
	}
	var result contract.ReleaseCheckpointResultV1
	if err := p.mutateRaw(ctx, broker.VerbDeploymentRelease, fence, 5, contract.ReleaseCheckpointRequestV1{CheckpointRef: checkpoint}, &result); err != nil {
		var public *broker.PublicError
		mapped := mapBrokerError(err)
		if errors.As(err, &public) {
			return definitiveCheckpointReleaseFailure(mapped)
		}
		return mapped
	}
	if result.CheckpointRef != checkpoint || result.ProviderRevision != contract.ProviderRevision {
		return ErrRevisionMismatch
	}
	return nil
}

func (p *SystemdBrokerProvider) Apply(ctx context.Context, fence FenceV1, target domain.ProfileID, checkpoint string) error {
	var result contract.ApplyResultV1
	if err := p.mutate(ctx, broker.VerbDeploymentApply, fence, 2, contract.ApplyRequestV1{TargetProfile: target, CheckpointRef: checkpoint}, &result); err != nil {
		return err
	}
	if result.TargetProfile != target || result.ProviderRevision != contract.ProviderRevision {
		return ErrRevisionMismatch
	}
	return nil
}

func (p *SystemdBrokerProvider) Verify(ctx context.Context, fence FenceV1, target domain.ProfileID, checkpoint string) (domain.Posture, error) {
	var result contract.VerifyResultV1
	if err := p.mutate(ctx, broker.VerbDeploymentVerify, fence, 3, contract.VerifyRequestV1{TargetProfile: target, CheckpointRef: checkpoint}, &result); err != nil {
		return domain.Posture{}, err
	}
	if !result.Verified || result.ProviderRevision != contract.ProviderRevision {
		return domain.Posture{}, ErrUnsafeMigration
	}
	return result.Posture, nil
}

func (p *SystemdBrokerProvider) Rollback(ctx context.Context, fence FenceV1, from domain.ProfileID, checkpoint string) (domain.Posture, error) {
	var result contract.RollbackResultV1
	if err := p.mutate(ctx, broker.VerbDeploymentRollback, fence, 4, contract.RollbackRequestV1{FromProfile: from, CheckpointRef: checkpoint}, &result); err != nil {
		return domain.Posture{}, err
	}
	if !result.Verified || result.ProviderRevision != contract.ProviderRevision {
		return domain.Posture{}, ErrUnsafeMigration
	}
	return result.Posture, nil
}

func (p *SystemdBrokerProvider) read(ctx context.Context, verb broker.Verb, operation string, payload, result any) error {
	if p == nil || p.client == nil {
		return ErrProviderUnavailable
	}
	_, err := p.client.Invoke(ctx, broker.Call{Verb: verb, OperationID: operation, Timeout: 15 * time.Second, Payload: payload}, result)
	return mapBrokerError(err)
}

func (p *SystemdBrokerProvider) mutate(ctx context.Context, verb broker.Verb, fence FenceV1, ordinal uint64, payload, result any) error {
	return mapBrokerError(p.mutateRaw(ctx, verb, fence, ordinal, payload, result))
}

func (p *SystemdBrokerProvider) mutateRaw(ctx context.Context, verb broker.Verb, fence FenceV1, ordinal uint64, payload, result any) error {
	if p == nil || p.client == nil || fence.OperationID == "" || fence.Revision == 0 || len(fence.Token) != 64 || len(fence.ExpectedPosture) != 64 {
		return ErrProviderUnavailable
	}
	data, _ := json.Marshal(struct {
		Verb    broker.Verb
		Fence   FenceV1
		Payload any
	}{verb, fence, payload})
	digest := broker.Digest(data)
	_, err := p.client.Invoke(ctx, broker.Call{Verb: verb, OperationID: fence.OperationID,
		IdempotencyKey: "deploy-" + digest[:48], Fence: broker.Fence{Resource: "deployment-profile", Sequence: fence.Revision*8 + ordinal, Token: fence.Token},
		Expected: broker.Revisions{Provider: contract.ProviderRevision, Configuration: fence.ExpectedPosture}, Timeout: MaxProviderDuration, Payload: payload}, result)
	return err
}

func mapBrokerError(err error) error {
	if err == nil {
		return nil
	}
	var public *broker.PublicError
	if errors.As(err, &public) && (public.Code == broker.CodeFence || public.Code == broker.CodeRevision || public.Code == broker.CodeIdempotency) {
		return ErrRevisionMismatch
	}
	return ErrProviderUnavailable
}

func containsVerbs(actual []broker.Verb, expected ...broker.Verb) bool {
	set := make(map[broker.Verb]bool, len(actual))
	for _, value := range actual {
		set[value] = true
	}
	for _, value := range expected {
		if !set[value] {
			return false
		}
	}
	return true
}
