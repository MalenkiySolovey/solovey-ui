package update

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestUpdateMutationWireEnvelopePassesBrokerAuthorityValidation(t *testing.T) {
	for _, verb := range []broker.Verb{broker.VerbUpdateStage, broker.VerbUpdateRelease,
		broker.VerbUpdatePrepare, broker.VerbUpdateActivate, broker.VerbUpdateVerify, broker.VerbUpdateRollback} {
		t.Run(string(verb), func(t *testing.T) {
			clientSide, serverSide := net.Pipe()
			t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
			now := time.Now().UTC()
			done := make(chan error, 1)
			go func() {
				var request broker.Request
				if err := broker.ReadFrame(serverSide, &request, broker.MaxRequestBytes); err != nil {
					done <- err
					return
				}
				err := request.Validate(now, broker.Definition{Role: broker.RolePanel, Mutation: true})
				response := broker.Response{ProtocolVersion: broker.ProtocolVersion, CapabilityRevision: broker.CapabilityRevision,
					RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb, OK: err == nil}
				if err != nil {
					response.Code, response.Message = broker.CodeInvalidRequest, "mutation envelope rejected"
				}
				if writeErr := broker.WriteFrame(serverSide, response, broker.MaxResponseBytes); err == nil {
					err = writeErr
				}
				done <- err
			}()
			provider := &BrokerProvider{Client: &broker.Client{Role: broker.RolePanel, BootID: "update-wire-fixture",
				Now:  func() time.Time { return now },
				Dial: func(context.Context, string) (net.Conn, error) { return clientSide, nil }}}
			operation := model.UpdateOperation{OperationID: "update-operation:wire-authority", Revision: 1,
				ManifestDigest: lifecycleDigest("wire-manifest"), DeploymentRevision: lifecycleDigest("wire-deployment")}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := provider.mutate(ctx, verb, operation, 1, struct{}{}, nil)
			if validationErr := <-done; validationErr != nil {
				t.Fatalf("real Update mutation envelope cannot pass the broker gate: %v", validationErr)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
