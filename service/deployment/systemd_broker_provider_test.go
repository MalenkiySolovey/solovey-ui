package deployment

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestCheckpointSystemdProviderUsesExactPrepareReplayAndTypedRelease(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	operationID := "deployment-operation:checkpoint-provider"
	checkpoint := domain.Revision("checkpoint-provider-checkpoint")
	connections := make(chan net.Conn, 3)
	requests := make(chan broker.Request, 3)
	serverErrors := make(chan error, 3)
	for index := 0; index < 3; index++ {
		clientConnection, serverConnection := net.Pipe()
		connections <- clientConnection
		go func() {
			defer serverConnection.Close()
			var request broker.Request
			if err := broker.ReadFrame(serverConnection, &request, broker.MaxRequestBytes); err != nil {
				serverErrors <- err
				return
			}
			requests <- request
			var result any
			switch request.Verb {
			case broker.VerbDeploymentPrepare:
				result = contract.PrepareResultV1{CheckpointRef: checkpoint, ProviderRevision: contract.ProviderRevision}
			case broker.VerbDeploymentRelease:
				var released contract.ReleaseCheckpointRequestV1
				if broker.DecodePayload(request.Payload, &released) != nil || released.CheckpointRef != checkpoint {
					serverErrors <- fmt.Errorf("unexpected checkpoint release payload: %#v", request)
					return
				}
				result = contract.ReleaseCheckpointResultV1{CheckpointRef: checkpoint, ProviderRevision: contract.ProviderRevision}
			default:
				serverErrors <- fmt.Errorf("unexpected deployment verb %q", request.Verb)
				return
			}
			payload, digest, err := broker.MarshalPayload(result)
			if err != nil {
				serverErrors <- err
				return
			}
			response := broker.Response{ProtocolVersion: broker.ProtocolVersion, CapabilityRevision: broker.CapabilityRevision,
				RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb, OK: true, Payload: payload, PayloadDigest: digest}
			serverErrors <- broker.WriteFrame(serverConnection, response, broker.MaxResponseBytes)
		}()
	}
	client := &broker.Client{Role: broker.RolePanel, SocketPath: "checkpoint-provider", BootID: "boot-checkpoint", Now: func() time.Time { return now },
		Dial: func(context.Context, string) (net.Conn, error) { return <-connections, nil }}
	provider := NewSystemdBrokerProvider(client)
	prepareFence := FenceV1{OperationID: operationID, Revision: 2, Token: domain.Revision("checkpoint-prepare-fence"),
		ExpectedPosture: domain.Revision("checkpoint-posture"), DeadlineAt: now.Add(MaxProviderDuration).Unix()}
	first, err := provider.Prepare(context.Background(), prepareFence, domain.NativeHardened)
	if err != nil || first != checkpoint {
		t.Fatalf("prepare checkpoint=%s err=%v", first, err)
	}
	recovered, err := provider.RecoverPreparedCheckpoint(context.Background(), prepareFence, domain.NativeHardened)
	if err != nil || recovered != checkpoint {
		t.Fatalf("recovered checkpoint=%s err=%v", recovered, err)
	}
	releaseFence := prepareFence
	releaseFence.Revision = 5
	if err := provider.ReleaseCheckpoint(context.Background(), releaseFence, checkpoint); err != nil {
		t.Fatal(err)
	}
	firstRequest, secondRequest, releaseRequest := <-requests, <-requests, <-requests
	if firstRequest.Verb != broker.VerbDeploymentPrepare || secondRequest.Verb != broker.VerbDeploymentPrepare ||
		firstRequest.OperationID != operationID || firstRequest.IdempotencyKey == "" || firstRequest.IdempotencyKey != secondRequest.IdempotencyKey ||
		firstRequest.PayloadDigest != secondRequest.PayloadDigest || firstRequest.Fence != secondRequest.Fence || firstRequest.Expected != secondRequest.Expected {
		t.Fatalf("prepare replay identities differ: first=%#v second=%#v", firstRequest, secondRequest)
	}
	if releaseRequest.Verb != broker.VerbDeploymentRelease || releaseRequest.OperationID != operationID || releaseRequest.IdempotencyKey == "" ||
		releaseRequest.Fence.Resource != "deployment-profile" || releaseRequest.Fence.Sequence != releaseFence.Revision*8+5 {
		t.Fatalf("typed release request=%#v", releaseRequest)
	}
	for index := 0; index < 3; index++ {
		if err := <-serverErrors; err != nil {
			t.Fatal(err)
		}
	}
}
