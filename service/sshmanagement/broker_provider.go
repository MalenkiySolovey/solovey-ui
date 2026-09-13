package sshmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshcontract "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type BrokerProvider struct{ client *broker.Client }

func NewBrokerProvider(client *broker.Client) *BrokerProvider {
	if client == nil {
		client = broker.NewClient(broker.RolePanel)
	}
	return &BrokerProvider{client: client}
}

func (p *BrokerProvider) ProviderID() string { return sshcontract.ProviderID }

func (p *BrokerProvider) Capabilities(ctx context.Context) domain.CapabilitySetV1 {
	result := domain.CapabilitySetV1{ObservePosture: domain.AvailabilityUnavailable, Prepare: domain.AvailabilityUnavailable,
		Stage: domain.AvailabilityUnavailable, Validate: domain.AvailabilityUnavailable, Reload: domain.AvailabilityUnavailable,
		Reconnect: domain.AvailabilityUnavailable, Rollback: domain.AvailabilityUnavailable,
		ReasonCodes: []domain.ReasonCode{domain.ReasonProviderUnavailable}}
	if p != nil && p.client != nil && runtime.GOOS == "linux" {
		var capabilities broker.CapabilitiesV1
		_, err := p.client.Invoke(ctx, broker.Call{Verb: broker.VerbCapabilities, OperationID: "ssh-capabilities",
			Timeout: 3 * time.Second, Payload: sshcontract.EmptyV1{}}, &capabilities)
		if err == nil && containsBrokerVerbs(capabilities.Verbs, broker.VerbSSHObserve, broker.VerbSSHPrepare, broker.VerbSSHStage, broker.VerbSSHRecoverStage, broker.VerbSSHReleaseStage, broker.VerbSSHValidate,
			broker.VerbSSHReload, broker.VerbSSHArm, broker.VerbSSHRestore, broker.VerbSSHInspect, broker.VerbSSHVerify) && len(capabilities.Unresolved) == 0 {
			result = domain.CapabilitySetV1{ObservePosture: domain.AvailabilityAvailable, Prepare: domain.AvailabilityAvailable,
				Stage: domain.AvailabilityAvailable, Validate: domain.AvailabilityAvailable, Reload: domain.AvailabilityAvailable,
				Reconnect: domain.AvailabilityAvailable, Rollback: domain.AvailabilityAvailable}
		}
	}
	result.Revision = domain.Revision(result)
	return result
}

func (p *BrokerProvider) Observe(ctx context.Context) (ObservationV1, error) {
	var result sshcontract.ObservationV1
	if err := p.read(ctx, broker.VerbSSHObserve, "ssh-observe", sshcontract.EmptyV1{}, &result); err != nil {
		return ObservationV1{}, providerError("observe", err)
	}
	return ObservationV1{Posture: result.Posture, ProviderRevision: result.ProviderRevision}, nil
}

func (p *BrokerProvider) PreparePolicy(ctx context.Context, request PrepareRequestV1) (PreparedPolicyV1, error) {
	var result sshcontract.PreparedPolicyV1
	if err := request.Policy.Validate(); err != nil {
		return PreparedPolicyV1{}, err
	}
	if err := p.read(ctx, broker.VerbSSHPrepare, "ssh-preview", sshcontract.PrepareRequestV1{Policy: request.Policy}, &result); err != nil {
		var public *broker.PublicError
		if errors.As(err, &public) && public.Code == broker.CodeValidation {
			return PreparedPolicyV1{}, domain.NewError("prepare", domain.ReasonUnsupportedDirective)
		}
		return PreparedPolicyV1{}, providerError("prepare", err)
	}
	return PreparedPolicyV1(result), nil
}

func (p *BrokerProvider) StageManagedPolicy(ctx context.Context, request StageRequestV1) (StageResultV1, error) {
	var result sshcontract.StageResultV1
	if request.EndpointID != request.Fence.EndpointID {
		return StageResultV1{}, domain.NewError("stage", domain.ReasonEndpointAmbiguous)
	}
	if err := p.mutate(ctx, broker.VerbSSHStage, request.Fence, 1, sshcontract.StageRequestV1{Policy: request.Policy,
		ExpectedArtifactDigest: request.ExpectedArtifactDigest, EndpointID: request.EndpointID}, &result); err != nil {
		return StageResultV1{}, providerError("stage", err)
	}
	return StageResultV1{ArtifactDigest: result.ArtifactDigest, EndpointID: result.EndpointID, Prior: providerPrior(result.Prior),
		ProviderRevision: result.ProviderRevision, ConfigurationRevision: result.ConfigurationRevision}, nil
}

func (p *BrokerProvider) RecoverCompletedStage(ctx context.Context, request RecoverStageRequestV1) (StageResultV1, error) {
	if err := request.Fence.Validate(time.Now().UTC()); err != nil {
		return StageResultV1{}, err
	}
	if request.EndpointID != request.Fence.EndpointID || request.ExpectedArtifactDigest != request.Fence.CandidateDigest {
		return StageResultV1{}, domain.NewError("recover_stage", domain.ReasonEndpointAmbiguous)
	}
	var result sshcontract.StageResultV1
	payload := sshcontract.RecoverStageRequestV1{EndpointID: request.EndpointID, ExpectedArtifactDigest: request.ExpectedArtifactDigest}
	if err := p.readExpected(ctx, broker.VerbSSHRecoverStage, request.Fence, payload, &result); err != nil {
		return StageResultV1{}, providerError("recover_stage", err)
	}
	return StageResultV1{ArtifactDigest: result.ArtifactDigest, EndpointID: result.EndpointID, Prior: providerPrior(result.Prior),
		ProviderRevision: result.ProviderRevision, ConfigurationRevision: result.ConfigurationRevision}, nil
}

func (p *BrokerProvider) ReleaseCompletedStage(ctx context.Context, request ReleaseStageRequestV1) error {
	if request.EndpointID != request.Fence.EndpointID || request.ExpectedArtifactDigest != request.Fence.CandidateDigest {
		return domain.NewError("release_stage", domain.ReasonEndpointAmbiguous)
	}
	payload := sshcontract.ReleaseStageRequestV1{EndpointID: request.EndpointID, ExpectedArtifactDigest: request.ExpectedArtifactDigest}
	return providerError("release_stage", p.mutate(ctx, broker.VerbSSHReleaseStage, request.Fence, 8, payload, nil))
}

func (p *BrokerProvider) ValidateManagedPolicy(ctx context.Context, request ValidationRequestV1) (ValidationResultV1, error) {
	var result sshcontract.ValidationResultV1
	if request.EndpointID != request.Fence.EndpointID {
		return ValidationResultV1{}, domain.NewError("validate", domain.ReasonEndpointAmbiguous)
	}
	if err := p.mutate(ctx, broker.VerbSSHValidate, request.Fence, 2, sshcontract.ValidationRequestV1{ArtifactDigest: request.ArtifactDigest, EndpointID: request.EndpointID}, &result); err != nil {
		return ValidationResultV1{}, providerError("validate", err)
	}
	return ValidationResultV1(result), nil
}

func (p *BrokerProvider) ReloadSelectedService(ctx context.Context, request ReloadRequestV1) (ReloadResultV1, error) {
	var result sshcontract.ReloadResultV1
	if request.EndpointID != request.Fence.EndpointID {
		return ReloadResultV1{}, domain.NewError("reload", domain.ReasonEndpointAmbiguous)
	}
	ordinal := uint64(3)
	if request.Recovery {
		ordinal = 7
	}
	if err := p.mutate(ctx, broker.VerbSSHReload, request.Fence, ordinal, sshcontract.ReloadRequestV1{ArtifactDigest: request.ArtifactDigest, EndpointID: request.EndpointID, Recovery: request.Recovery}, &result); err != nil {
		return ReloadResultV1{}, providerError("reload", err)
	}
	return ReloadResultV1(result), nil
}

// ArmReconnect persists the secret verifier only in the root-owned proof
// journal. It is retrieved through the separately attested proof socket.
func (p *BrokerProvider) ArmReconnect(ctx context.Context, proof ReconnectProofV1, expiresAt int64) error {
	if proof.EndpointID != proof.Fence.EndpointID {
		return domain.NewError("arm_reconnect", domain.ReasonEndpointAmbiguous)
	}
	payload := sshcontract.ArmRequestV1{MarkerDigest: proof.MarkerDigest, Verifier: proof.Verifier,
		EndpointID: proof.EndpointID, PrincipalID: proof.PrincipalID, AuthenticationClass: proof.AuthenticationClass, ExpiresAt: expiresAt}
	return providerError("arm_reconnect", p.mutate(ctx, broker.VerbSSHArm, proof.Fence, 4, payload, nil))
}

func (p *BrokerProvider) VerifyReconnect(ctx context.Context, request ReconnectProofV1) (ReconnectResultV1, error) {
	if request.EndpointID != request.Fence.EndpointID {
		return ReconnectResultV1{}, domain.NewError("reconnect", domain.ReasonEndpointAmbiguous)
	}
	var result sshcontract.VerifyResultV1
	payload := sshcontract.VerifyRequestV1{MarkerDigest: request.MarkerDigest, Verifier: request.Verifier,
		EndpointID: request.EndpointID, PrincipalID: request.PrincipalID, AuthenticationClass: request.AuthenticationClass}
	if err := p.mutate(ctx, broker.VerbSSHVerify, request.Fence, 6, payload, &result); err != nil {
		return ReconnectResultV1{}, providerError("reconnect", err)
	}
	return ReconnectResultV1(result), nil
}

func (p *BrokerProvider) RestoreManagedPolicy(ctx context.Context, request RestoreRequestV1) (RestoreResultV1, error) {
	var result sshcontract.RestoreResultV1
	if request.EndpointID != request.Fence.EndpointID {
		return RestoreResultV1{}, domain.NewError("restore", domain.ReasonEndpointAmbiguous)
	}
	payload := sshcontract.RestoreRequestV1{ExpectedCurrentArtifactDigest: request.ExpectedCurrentArtifactDigest, EndpointID: request.EndpointID, Prior: contractPrior(request.Prior)}
	if err := p.mutate(ctx, broker.VerbSSHRestore, request.Fence, 5, payload, &result); err != nil {
		return RestoreResultV1{}, providerError("restore", err)
	}
	return RestoreResultV1(result), nil
}

func (p *BrokerProvider) InspectManagedPolicy(ctx context.Context, request InspectRequestV1) (InspectResultV1, error) {
	if err := request.Fence.Validate(time.Now().UTC()); err != nil {
		return InspectResultV1{}, err
	}
	var result sshcontract.InspectResultV1
	if request.EndpointID != request.Fence.EndpointID {
		return InspectResultV1{}, domain.NewError("inspect", domain.ReasonEndpointAmbiguous)
	}
	if err := p.readExpected(ctx, broker.VerbSSHInspect, request.Fence, sshcontract.InspectRequestV1{EndpointID: request.EndpointID}, &result); err != nil {
		return InspectResultV1{}, providerError("inspect", err)
	}
	return InspectResultV1(result), nil
}

func (p *BrokerProvider) read(ctx context.Context, verb broker.Verb, operationID string, payload, target any) error {
	if p == nil || p.client == nil {
		return errors.New("broker client is absent")
	}
	_, err := p.client.Invoke(ctx, broker.Call{Verb: verb, OperationID: operationID, Timeout: MaxProviderRequestDuration, Payload: payload}, target)
	return err
}

func (p *BrokerProvider) readExpected(ctx context.Context, verb broker.Verb, fence ProviderFenceV1, payload, target any) error {
	if p == nil || p.client == nil {
		return errors.New("broker client is absent")
	}
	_, err := p.client.Invoke(ctx, broker.Call{Verb: verb, OperationID: fence.OperationID, Expected: brokerRevisions(fence),
		Timeout: MaxProviderRequestDuration, Payload: payload}, target)
	return err
}

func (p *BrokerProvider) mutate(ctx context.Context, verb broker.Verb, fence ProviderFenceV1, ordinal uint64, payload, target any) error {
	if p == nil || p.client == nil {
		return errors.New("broker client is absent")
	}
	if err := fence.Validate(time.Now().UTC()); err != nil {
		return err
	}
	data, _ := json.Marshal(struct {
		Verb    broker.Verb
		Fence   ProviderFenceV1
		Payload any
	}{verb, fence, payload})
	digest := broker.Digest(data)
	call := broker.Call{Verb: verb, OperationID: fence.OperationID, IdempotencyKey: "ssh-" + digest[:48],
		Fence:    broker.Fence{Resource: "ssh-managed-dropin", Sequence: fence.CandidateRevision*16 + ordinal, Token: fence.FencingToken},
		Expected: brokerRevisions(fence), Timeout: MaxProviderRequestDuration, Payload: payload}
	_, err := p.client.Invoke(ctx, call, target)
	return err
}

func brokerRevisions(fence ProviderFenceV1) broker.Revisions {
	return broker.Revisions{Provider: fence.ExpectedProviderRevision, Binary: fence.ExpectedBinaryRevision,
		Service: fence.ExpectedServiceRevision, Configuration: fence.ExpectedConfigurationRevision}
}

func providerPrior(value sshcontract.PriorArtifactV1) PriorArtifactV1 {
	return PriorArtifactV1{Present: value.Present, Content: append([]byte(nil), value.Content...), Owner: value.Owner,
		Group: value.Group, ModeClass: value.ModeClass, Mode: value.Mode, Digest: value.Digest}
}

func contractPrior(value PriorArtifactV1) sshcontract.PriorArtifactV1 {
	return sshcontract.PriorArtifactV1{Present: value.Present, Content: append([]byte(nil), value.Content...), Owner: value.Owner,
		Group: value.Group, ModeClass: value.ModeClass, Mode: value.Mode, Digest: value.Digest}
}

func providerError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var public *broker.PublicError
	if errors.As(err, &public) && public.Code == broker.CodeRevision || public != nil && public.Code == broker.CodeFence {
		return domain.NewError(operation, domain.ReasonRevisionMismatch)
	}
	return domain.NewError(operation, domain.ReasonProviderUnavailable)
}

func containsBrokerVerbs(actual []broker.Verb, expected ...broker.Verb) bool {
	available := make(map[broker.Verb]bool, len(actual))
	for _, value := range actual {
		available[value] = true
	}
	for _, value := range expected {
		if !available[value] {
			return false
		}
	}
	return true
}
