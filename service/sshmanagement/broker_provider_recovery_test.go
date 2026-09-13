package sshmanagement

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshcontract "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestRecoveryBrokerProviderQueriesExactCompletedStageGeneration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	endpointID := "management:ssh:configured:ipv4:22"
	candidateDigest := domain.Revision("recovery-candidate")
	priorContent := []byte("prior managed drop-in")
	prior := sshcontract.PriorArtifactV1{Present: true, Content: priorContent, Owner: "root", Group: "root", ModeClass: "owner_read_write", Mode: 0o600, Digest: domain.Revision(priorContent)}
	want := sshcontract.StageResultV1{ArtifactDigest: candidateDigest, EndpointID: endpointID, Prior: prior,
		ProviderRevision: sshcontract.ProviderRevision, ConfigurationRevision: domain.Revision("recovery-staged-configuration")}
	clientConnection, serverConnection := net.Pipe()
	serverErr := make(chan error, 1)
	go func() {
		defer serverConnection.Close()
		var request broker.Request
		if err := broker.ReadFrame(serverConnection, &request, broker.MaxRequestBytes); err != nil {
			serverErr <- err
			return
		}
		var recovered sshcontract.RecoverStageRequestV1
		if request.Verb != broker.VerbSSHRecoverStage || request.OperationID != "ssh-operation:recovery-provider" ||
			broker.DecodePayload(request.Payload, &recovered) != nil || recovered.EndpointID != endpointID || recovered.ExpectedArtifactDigest != candidateDigest {
			serverErr <- fmt.Errorf("unexpected recovery request: %#v payload=%#v", request, recovered)
			return
		}
		payload, digest, err := broker.MarshalPayload(want)
		if err != nil {
			serverErr <- err
			return
		}
		response := broker.Response{ProtocolVersion: broker.ProtocolVersion, CapabilityRevision: broker.CapabilityRevision,
			RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb, OK: true, Payload: payload, PayloadDigest: digest}
		serverErr <- broker.WriteFrame(serverConnection, response, broker.MaxResponseBytes)
	}()
	client := &broker.Client{Role: broker.RolePanel, SocketPath: "recovery-test", BootID: "boot-recovery", Now: func() time.Time { return now },
		Dial: func(context.Context, string) (net.Conn, error) { return clientConnection, nil }}
	provider := NewBrokerProvider(client)
	fence := ProviderFenceV1{OperationID: "ssh-operation:recovery-provider", EndpointID: endpointID, CandidateRevision: 2,
		FencingToken: domain.Revision("recovery-fence"), CandidateDigest: candidateDigest, ExpectedProviderRevision: sshcontract.ProviderRevision,
		ExpectedBinaryRevision: domain.Revision("recovery-binary"), ExpectedServiceRevision: domain.Revision("recovery-service"),
		ExpectedConfigurationRevision: domain.Revision("recovery-configuration"), DeadlineAt: now.Add(MaxProviderRequestDuration).Unix()}
	got, err := provider.RecoverCompletedStage(context.Background(), RecoverStageRequestV1{Fence: fence, EndpointID: endpointID, ExpectedArtifactDigest: candidateDigest})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	if got.ArtifactDigest != want.ArtifactDigest || got.EndpointID != want.EndpointID || got.Prior.Digest != want.Prior.Digest ||
		got.ProviderRevision != want.ProviderRevision || got.ConfigurationRevision != want.ConfigurationRevision {
		t.Fatalf("completed stage generation=%#v, want %#v", got, want)
	}
}

func TestRetentionBrokerProviderPublishesTypedCompletedStageRelease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	endpointID := "management:ssh:configured:ipv4:22"
	candidateDigest := domain.Revision("retention-candidate")
	clientConnection, serverConnection := net.Pipe()
	serverErr := make(chan error, 1)
	go func() {
		defer serverConnection.Close()
		var request broker.Request
		if err := broker.ReadFrame(serverConnection, &request, broker.MaxRequestBytes); err != nil {
			serverErr <- err
			return
		}
		var released sshcontract.ReleaseStageRequestV1
		if request.Verb != broker.VerbSSHReleaseStage || request.OperationID != "ssh-operation:retention-provider" || request.IdempotencyKey == "" ||
			request.Fence.Resource != "ssh-managed-dropin" || request.Fence.Sequence != 7*16+8 ||
			broker.DecodePayload(request.Payload, &released) != nil || released.EndpointID != endpointID || released.ExpectedArtifactDigest != candidateDigest {
			serverErr <- fmt.Errorf("unexpected release request: %#v payload=%#v", request, released)
			return
		}
		payload, digest, err := broker.MarshalPayload(sshcontract.EmptyV1{})
		if err != nil {
			serverErr <- err
			return
		}
		response := broker.Response{ProtocolVersion: broker.ProtocolVersion, CapabilityRevision: broker.CapabilityRevision,
			RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb, OK: true, Payload: payload, PayloadDigest: digest}
		serverErr <- broker.WriteFrame(serverConnection, response, broker.MaxResponseBytes)
	}()
	client := &broker.Client{Role: broker.RolePanel, SocketPath: "retention-test", BootID: "boot-retention", Now: func() time.Time { return now },
		Dial: func(context.Context, string) (net.Conn, error) { return clientConnection, nil }}
	provider := NewBrokerProvider(client)
	fence := ProviderFenceV1{OperationID: "ssh-operation:retention-provider", EndpointID: endpointID, CandidateRevision: 7,
		FencingToken: domain.Revision("retention-fence"), CandidateDigest: candidateDigest, ExpectedProviderRevision: sshcontract.ProviderRevision,
		ExpectedBinaryRevision: domain.Revision("retention-binary"), ExpectedServiceRevision: domain.Revision("retention-service"),
		ExpectedConfigurationRevision: domain.Revision("retention-configuration"), DeadlineAt: now.Add(MaxProviderRequestDuration).Unix()}
	if err := provider.ReleaseCompletedStage(context.Background(), ReleaseStageRequestV1{Fence: fence, EndpointID: endpointID, ExpectedArtifactDigest: candidateDigest}); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}
