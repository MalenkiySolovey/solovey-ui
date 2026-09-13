package sshbroker

import (
	"errors"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type completedMutationAuthorityFake struct {
	response          broker.Response
	err               error
	expectedOperation string
	expectedVerb      broker.Verb
	expectedResource  string
}

func (f completedMutationAuthorityFake) CompletedMutation(operation string, verb broker.Verb, resource string) (broker.Response, error) {
	if f.expectedOperation != "" && (operation != f.expectedOperation || verb != f.expectedVerb || resource != f.expectedResource) {
		return broker.Response{}, errors.New("completed mutation identity mismatch")
	}
	return f.response, f.err
}

func TestRollbackPriorUsesCompletedBrokerStageAuthority(t *testing.T) {
	candidateDigest := broker.Digest([]byte("candidate"))
	priorDigest := broker.Digest([]byte("prior-artifact"))
	prior := PriorArtifactV1{Present: true, Content: []byte(`{"options":{"Port":"22","UnrelatedCustom":"preserve me"}}`),
		Owner: "root", Group: "root", ModeClass: "owner_read_write", Mode: 0o600, Digest: priorDigest}
	endpointID := "management:ssh:configured:ipv4:22"
	staged := StageResultV1{ArtifactDigest: candidateDigest, EndpointID: endpointID, Prior: prior, ProviderRevision: ProviderRevision,
		ConfigurationRevision: broker.Digest([]byte("configuration"))}
	payload, payloadDigest, err := broker.MarshalPayload(staged)
	if err != nil {
		t.Fatal(err)
	}
	authority := completedMutationAuthorityFake{response: broker.Response{OK: true, Payload: payload, PayloadDigest: payloadDigest}}
	envelope := broker.Request{OperationID: "ssh-operation:authority", Fence: broker.Fence{Resource: "ssh-managed-dropin"}}
	request := RestoreRequestV1{ExpectedCurrentArtifactDigest: candidateDigest, EndpointID: endpointID, Prior: prior}

	resolved, err := authoritativeDropbearStagePrior(authority, envelope, request)
	if err != nil || !samePriorArtifact(resolved.Prior, prior) || string(resolved.Prior.Content) != string(prior.Content) {
		t.Fatalf("valid broker stage authority was not recovered: %#v err=%v", resolved, err)
	}

	request.Prior.Content = []byte(`{"options":{"Port":"22","ArbitraryInjected":"caller chose this"}}`)
	request.Prior.Digest = broker.Digest(request.Prior.Content)
	if _, err := authoritativeDropbearStagePrior(authority, envelope, request); brokerFailureCode(err) != broker.CodeInvalidRequest {
		t.Fatalf("forged self-consistent caller checkpoint was not rejected: %v", err)
	}

	request.Prior = prior
	if _, err := authoritativeDropbearStagePrior(completedMutationAuthorityFake{err: errors.New("missing")}, envelope, request); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("missing broker checkpoint did not fail closed: %v", err)
	}

	corrupt := authority
	corrupt.response.Payload = append([]byte(nil), corrupt.response.Payload...)
	corrupt.response.Payload[0] = '['
	if _, err := authoritativeDropbearStagePrior(corrupt, envelope, request); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("corrupt broker checkpoint did not fail closed: %v", err)
	}
}

func TestCompletionLifecycleRetainsEveryStageUntilTypedRelease(t *testing.T) {
	for _, implementation := range []Implementation{ImplementationOpenSSH, ImplementationDropbear} {
		stage := completedMutationLifecycle(implementation, broker.VerbSSHStage)
		if !stage.RetainUntilRelease || stage.ReleasesVerb != "" {
			t.Fatalf("%s stage lifecycle=%#v", implementation, stage)
		}
		restore := completedMutationLifecycle(implementation, broker.VerbSSHRestore)
		if restore.RetainUntilRelease || restore.ReleasesVerb != broker.VerbSSHStage {
			t.Fatalf("%s restore lifecycle=%#v", implementation, restore)
		}
		release := completedMutationLifecycle(implementation, broker.VerbSSHReleaseStage)
		if release.RetainUntilRelease || release.ReleasesVerb != broker.VerbSSHStage {
			t.Fatalf("%s explicit release lifecycle=%#v", implementation, release)
		}
	}
	for _, fixture := range []struct {
		implementation Implementation
		verb           broker.Verb
	}{
		{ImplementationOpenSSH, broker.VerbSSHReload},
		{ImplementationDropbear, broker.VerbSSHReload},
	} {
		if policy := completedMutationLifecycle(fixture.implementation, fixture.verb); policy != (broker.CompletionPolicy{}) {
			t.Fatalf("unrelated completion lifecycle was broadened: implementation=%s verb=%s policy=%#v", fixture.implementation, fixture.verb, policy)
		}
	}
}

func brokerFailureCode(err error) broker.ErrorCode {
	var failure *broker.PublicError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
