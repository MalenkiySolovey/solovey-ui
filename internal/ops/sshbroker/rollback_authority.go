package sshbroker

import (
	"bytes"
	"encoding/hex"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func completedMutationLifecycle(implementation Implementation, verb broker.Verb) broker.CompletionPolicy {
	_ = implementation
	switch verb {
	case broker.VerbSSHStage:
		return broker.CompletionPolicy{RetainUntilRelease: true}
	case broker.VerbSSHRestore:
		return broker.CompletionPolicy{ReleasesVerb: broker.VerbSSHStage}
	case broker.VerbSSHReleaseStage:
		return broker.CompletionPolicy{ReleasesVerb: broker.VerbSSHStage}
	default:
		return broker.CompletionPolicy{}
	}
}

func authoritativeStageResult(authority broker.CompletedMutationAuthority, operationID, endpointID, expectedArtifactDigest string, implementation Implementation) (StageResultV1, error) {
	if authority == nil {
		return StageResultV1{}, broker.Failure(broker.CodeRecoveryRequired, "SSH stage recovery authority is unavailable")
	}
	response, err := authority.CompletedMutation(operationID, broker.VerbSSHStage, "ssh-managed-dropin")
	if err != nil {
		return StageResultV1{}, broker.Failure(broker.CodeRecoveryRequired, "SSH stage recovery authority is unavailable")
	}
	var staged StageResultV1
	if !response.OK || response.PayloadDigest != broker.Digest(response.Payload) || broker.DecodePayload(response.Payload, &staged) != nil ||
		staged.EndpointID != endpointID || staged.ProviderRevision != ProviderRevision || staged.ArtifactDigest != expectedArtifactDigest ||
		!contractDigest(staged.ConfigurationRevision) || !validCompletedStagePrior(staged.Prior, implementation) {
		return StageResultV1{}, broker.Failure(broker.CodeRecoveryRequired, "SSH stage recovery authority is corrupt")
	}
	return staged, nil
}

func validCompletedStagePrior(prior PriorArtifactV1, implementation Implementation) bool {
	if prior.Owner != "root" || prior.Group != "root" || prior.ModeClass != "owner_read_write" ||
		(prior.Mode != 0o600 && prior.Mode != 0o640 && prior.Mode != 0o644) || !contractDigest(prior.Digest) {
		return false
	}
	if implementation == ImplementationDropbear {
		return prior.Present && prior.Mode == 0o600 && len(prior.Content) > 0 && len(prior.Content) <= MaxCheckpointBytes
	}
	if implementation != ImplementationOpenSSH {
		return false
	}
	if prior.Present {
		return len(prior.Content) > 0 && len(prior.Content) <= MaxDropInBytes && prior.Digest == domain.Revision(prior.Content)
	}
	return len(prior.Content) == 0 && prior.Digest == domain.Revision([]byte{})
}

func authoritativeDropbearStagePrior(authority broker.CompletedMutationAuthority, envelope broker.Request, request RestoreRequestV1) (StageResultV1, error) {
	staged, err := authoritativeStageResult(authority, envelope.OperationID, request.EndpointID, request.ExpectedCurrentArtifactDigest, ImplementationDropbear)
	if err != nil {
		return StageResultV1{}, err
	}
	if !samePriorArtifact(request.Prior, staged.Prior) {
		return StageResultV1{}, broker.Failure(broker.CodeInvalidRequest, "SSH rollback checkpoint differs from broker authority")
	}
	return staged, nil
}

func samePriorArtifact(left, right PriorArtifactV1) bool {
	return left.Present == right.Present && bytes.Equal(left.Content, right.Content) && left.Owner == right.Owner && left.Group == right.Group &&
		left.ModeClass == right.ModeClass && left.Mode == right.Mode && left.Digest == right.Digest
}

func contractDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == bytesToLowerASCII(value)
}

func bytesToLowerASCII(value string) string {
	result := make([]byte, len(value))
	for index := range value {
		character := value[index]
		if character >= 'A' && character <= 'F' {
			character += 'a' - 'A'
		}
		result[index] = character
	}
	return string(result)
}
