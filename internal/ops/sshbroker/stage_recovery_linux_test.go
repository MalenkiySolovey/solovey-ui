//go:build linux

package sshbroker

import (
	"context"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestRecoveryOpenSSHCompletedStageLookupIsOperationEndpointAndArtifactBound(t *testing.T) {
	operationID := "ssh-operation:recovery-openssh"
	endpointID := "management:ssh:configured:ipv4:22"
	artifactDigest := domain.Revision("recovery-openssh-candidate")
	priorContent := []byte("prior managed OpenSSH drop-in")
	stage := StageResultV1{ArtifactDigest: artifactDigest, EndpointID: endpointID,
		Prior:            PriorArtifactV1{Present: true, Content: priorContent, Owner: "root", Group: "root", ModeClass: "owner_read_write", Mode: 0o600, Digest: domain.Revision(priorContent)},
		ProviderRevision: ProviderRevision, ConfigurationRevision: domain.Revision("recovery-openssh-staged")}
	payload, digest, err := broker.MarshalPayload(stage)
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{implementation: ImplementationOpenSSH, stageAuthority: completedMutationAuthorityFake{
		response: broker.Response{OK: true, Payload: payload, PayloadDigest: digest}, expectedOperation: operationID,
		expectedVerb: broker.VerbSSHStage, expectedResource: "ssh-managed-dropin",
	}}
	requestPayload, requestDigest, err := broker.MarshalPayload(RecoverStageRequestV1{EndpointID: endpointID, ExpectedArtifactDigest: artifactDigest})
	if err != nil {
		t.Fatal(err)
	}
	envelope := broker.Request{OperationID: operationID, Payload: requestPayload, PayloadDigest: requestDigest}
	got, err := host.recoverStageHandler(context.Background(), envelope, broker.PeerIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	recovered, ok := got.(StageResultV1)
	if !ok || recovered.EndpointID != endpointID || recovered.ArtifactDigest != artifactDigest || !samePriorArtifact(recovered.Prior, stage.Prior) {
		t.Fatalf("recovered stage=%#v", got)
	}
	for _, mismatched := range []RecoverStageRequestV1{
		{EndpointID: "management:ssh:configured:ipv4:2222", ExpectedArtifactDigest: artifactDigest},
		{EndpointID: endpointID, ExpectedArtifactDigest: domain.Revision("different-artifact")},
	} {
		requestPayload, requestDigest, err = broker.MarshalPayload(mismatched)
		if err != nil {
			t.Fatal(err)
		}
		envelope.Payload, envelope.PayloadDigest = requestPayload, requestDigest
		if _, err := host.recoverStageHandler(context.Background(), envelope, broker.PeerIdentity{}); brokerFailureCode(err) != broker.CodeRecoveryRequired {
			t.Fatalf("mismatched completed stage did not fail closed: request=%#v err=%v", mismatched, err)
		}
	}
}

func TestRetentionCompletedStageReleaseIsOperationEndpointAndArtifactBound(t *testing.T) {
	operationID := "ssh-operation:retention-release"
	endpointID := "management:ssh:configured:ipv4:22"
	artifactDigest := domain.Revision("retention-release-candidate")
	stage := StageResultV1{ArtifactDigest: artifactDigest, EndpointID: endpointID,
		Prior: PriorArtifactV1{Present: false, Owner: "root", Group: "root", ModeClass: "owner_read_write", Mode: 0o600, Digest: domain.Revision([]byte{})}, ProviderRevision: ProviderRevision,
		ConfigurationRevision: domain.Revision("retention-release-configuration")}
	payload, digest, err := broker.MarshalPayload(stage)
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{implementation: ImplementationOpenSSH, stageAuthority: completedMutationAuthorityFake{
		response: broker.Response{OK: true, Payload: payload, PayloadDigest: digest}, expectedOperation: operationID,
		expectedVerb: broker.VerbSSHStage, expectedResource: "ssh-managed-dropin",
	}}
	for _, fixture := range []struct {
		request ReleaseStageRequestV1
		ok      bool
	}{
		{ReleaseStageRequestV1{EndpointID: endpointID, ExpectedArtifactDigest: artifactDigest}, true},
		{ReleaseStageRequestV1{EndpointID: endpointID + ":foreign", ExpectedArtifactDigest: artifactDigest}, false},
		{ReleaseStageRequestV1{EndpointID: endpointID, ExpectedArtifactDigest: domain.Revision("foreign")}, false},
	} {
		requestPayload, requestDigest, marshalErr := broker.MarshalPayload(fixture.request)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		_, releaseErr := host.releaseStageHandler(context.Background(), broker.Request{OperationID: operationID, Payload: requestPayload, PayloadDigest: requestDigest}, broker.PeerIdentity{})
		if (releaseErr == nil) != fixture.ok {
			t.Fatalf("release request=%#v err=%v", fixture.request, releaseErr)
		}
	}
}
