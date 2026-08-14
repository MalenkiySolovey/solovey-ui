package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const brokerVerbPrefix = "server-protection."

type BrokerInvoker struct{ client *broker.Client }

func NewBrokerInvoker(client *broker.Client) *BrokerInvoker {
	if client == nil {
		client = broker.NewClient(broker.RolePanel)
	}
	return &BrokerInvoker{client: client}
}

// DiscoverInstalledBrokerInvoker selects only the fixed production broker
// socket. Socket ownership and the root peer credential are re-attested by the
// broker client on every connection; no environment or setting can redirect
// this authority.
func DiscoverInstalledBrokerInvoker() (Invoker, string) {
	if runtime.GOOS != "linux" {
		return nil, "linux_required"
	}
	return NewBrokerInvoker(nil), ""
}

func (b *BrokerInvoker) Invoke(ctx context.Context, request Request) (Response, InvocationFacts, error) {
	if b == nil || b.client == nil {
		return Response{}, InvocationFacts{ExitClass: "not_started"}, errors.New("privileged broker client is not configured")
	}
	verb, ordinal, ok := brokerVerb(request.Operation)
	if !ok {
		return Response{}, InvocationFacts{ExitClass: "not_started"}, errors.New("server-protection broker verb is not allowlisted")
	}
	call := broker.Call{Verb: verb, OperationID: request.Correlation.OperationID, Timeout: timeoutFor(request.Operation), Payload: request}
	if !request.UnlockedReadOnly() {
		encoded, _ := json.Marshal(request)
		digest := broker.Digest(encoded)
		call.IdempotencyKey = "sp-" + digest[:48]
		call.Fence = broker.Fence{Resource: "sp-" + broker.Digest([]byte(request.Correlation.OperationID + "\x00" + request.Correlation.InstanceID))[:48],
			Sequence: uint64(request.Correlation.LockRevision)*32 + uint64(ordinal), Token: digest}
	}
	var response Response
	receipt, err := b.client.Invoke(ctx, call, &response)
	facts := InvocationFacts{ExitClass: "ok"}
	if err != nil {
		facts.ExitClass = "broker_failed"
		return Response{}, facts, err
	}
	if receipt != nil && receipt.Outcome != "succeeded" {
		facts.ExitClass = "broker_failed"
	}
	return response, facts, nil
}

func (b *BrokerInvoker) HelperIdentityRevision() string {
	return broker.Digest([]byte("solovey-privileged-broker:" + broker.CapabilityRevision))
}

func RegisterBrokerHandlers(registry *broker.Registry, root ManagedRoot) error {
	if registry == nil || root.Path() == "" {
		return errors.New("server-protection broker dependencies are required")
	}
	engine := NewContractEngine(root)
	for _, operation := range []Operation{OperationCapabilities, OperationNFTValidate, OperationNFTApply, OperationNFTRollback,
		OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch,
		OperationNginxReload, OperationNginxVerify, OperationNginxRestore,
		OperationListenerOwnerObserve, OperationSSHRecoveryObserve, OperationArtifact} {
		operation := operation
		verb, _, _ := brokerVerb(operation)
		definition := broker.Definition{Role: broker.RolePanel, Mutation: !unlockedOperation(operation)}
		definition.Handler = func(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
			var request Request
			if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || request.Operation != operation {
				return nil, broker.Failure(broker.CodeInvalidRequest, "server-protection payload does not match its typed verb")
			}
			if err := request.Validate(root); err != nil {
				return nil, broker.Failure(broker.CodeValidation, "server-protection request validation failed")
			}
			return engine.HandleContext(ctx, request), nil
		}
		if err := registry.Register(verb, definition); err != nil {
			return fmt.Errorf("register %s: %w", operation, err)
		}
	}
	return nil
}

func unlockedOperation(operation Operation) bool {
	return operation == OperationCapabilities || operation == OperationSSHRecoveryObserve || operation == OperationListenerOwnerObserve
}

func brokerVerb(operation Operation) (broker.Verb, int, bool) {
	operations := []Operation{OperationCapabilities, OperationNFTValidate, OperationNFTApply, OperationNFTRollback,
		OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch,
		OperationNginxReload, OperationNginxVerify, OperationNginxRestore,
		OperationListenerOwnerObserve, OperationSSHRecoveryObserve, OperationArtifact}
	for index, candidate := range operations {
		if candidate == operation {
			return broker.Verb(brokerVerbPrefix + string(operation)), index + 1, true
		}
	}
	return "", 0, false
}
