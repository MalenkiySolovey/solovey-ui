package helperinvoker

import (
	"context"
	"errors"
	"strings"
	"testing"

	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

func TestNginxInvokerModelsTypedSequenceIdempotencyAndRestore(t *testing.T) {
	invoker := NewNginx()
	previous, previousSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	target, targetSHA := strings.Repeat("c", 64), strings.Repeat("d", 64)
	previousListeners := []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 443}}
	targetListeners := []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 8443}}
	invoker.ActiveRevision, invoker.ActiveSHA256 = previous, previousSHA
	invoker.Revisions[previous] = previousSHA
	invoker.RevisionListeners[previous] = previousListeners
	correlation := protectionhelper.Correlation{OperationID: "normal-ci-nginx", InstanceID: "test", LockRevision: 1}

	capabilityRequest := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationCapabilities, Capabilities: &protectionhelper.CapabilitiesRequest{}}
	capabilityResponse, _, err := invoker.Invoke(context.Background(), capabilityRequest)
	if err != nil || !capabilityResponse.OK || protectionhelper.ValidateCapabilities(capabilityResponse.Capabilities) != nil {
		t.Fatalf("capabilities=%#v err=%v", capabilityResponse, err)
	}

	requests := []protectionhelper.Request{
		{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxValidate, NginxValidate: &protectionhelper.NginxValidateRequest{ExpectedRevision: target, ExpectedSHA256: targetSHA, ExpectedBinary: invoker.Support.Binary}},
		{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxInstall, NginxInstall: &protectionhelper.NginxInstallRequest{ExpectedRevision: target, ExpectedSHA256: targetSHA, Listeners: targetListeners}},
		{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxSwitch, NginxSwitch: &protectionhelper.NginxSwitchRequest{ExpectedPreviousRevision: previous, TargetRevision: target, ExpectedSHA256: targetSHA}},
		{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: target, ExpectedSHA256: targetSHA, ExpectedBinary: invoker.Support.Binary}},
		{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: target, ExpectedSHA256: targetSHA, ExpectedBinary: invoker.Support.Binary, Listeners: targetListeners}},
	}
	for _, request := range requests {
		response, _, invokeErr := invoker.Invoke(context.Background(), request)
		if invokeErr != nil || !response.OK {
			t.Fatalf("operation=%s response=%#v err=%v", request.Operation, response, invokeErr)
		}
	}
	reloadResponse, _, err := invoker.Invoke(context.Background(), requests[3])
	if err != nil || !reloadResponse.OK || invoker.Reloads != 1 || reloadResponse.Nginx == nil || len(reloadResponse.Nginx.Diagnostics) != 1 || reloadResponse.Nginx.Diagnostics[0] != "reload_idempotent_replay" {
		t.Fatalf("reload=%#v count=%d err=%v", reloadResponse, invoker.Reloads, err)
	}
	restore := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxRestore, NginxRestore: &protectionhelper.NginxRestoreRequest{ExpectedCurrentRevision: target, PreviousRevision: previous, ExpectedSHA256: previousSHA}}
	response, _, err := invoker.Invoke(context.Background(), restore)
	if err != nil || !response.OK || invoker.ActiveRevision != previous || invoker.ActiveSHA256 != previousSHA {
		t.Fatalf("restore=%#v active=%s sha=%s err=%v", response, invoker.ActiveRevision, invoker.ActiveSHA256, err)
	}
}

func TestNginxInvokerFailureBoundariesAreDeterministic(t *testing.T) {
	invoker := NewNginx()
	previous, previousSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	target, targetSHA := strings.Repeat("c", 64), strings.Repeat("d", 64)
	invoker.ActiveRevision, invoker.ActiveSHA256 = previous, previousSHA
	invoker.Revisions[target] = targetSHA
	invoker.FailAfter[protectionhelper.OperationNginxSwitch] = errors.New("lost response")
	request := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: protectionhelper.Correlation{OperationID: "lost-switch", InstanceID: "test", LockRevision: 1}, Operation: protectionhelper.OperationNginxSwitch, NginxSwitch: &protectionhelper.NginxSwitchRequest{ExpectedPreviousRevision: previous, TargetRevision: target, ExpectedSHA256: targetSHA}}
	response, _, err := invoker.Invoke(context.Background(), request)
	if err != nil || response.OK || invoker.ActiveRevision != target || invoker.ActiveSHA256 != targetSHA {
		t.Fatalf("lost switch response=%#v active=%s sha=%s err=%v", response, invoker.ActiveRevision, invoker.ActiveSHA256, err)
	}
}

func TestGenericInvokerModelsOnlyExplicitTypedResponses(t *testing.T) {
	capabilities := protectionhelper.DefaultCapabilities()
	capabilities.NFT = protectionhelper.NFTSupport{PlatformKnown: true, Linux: true, Available: true}
	for index := range capabilities.Capabilities {
		switch capabilities.Capabilities[index].Operation {
		case protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback:
			capabilities.Capabilities[index].Available = true
			capabilities.Capabilities[index].Reason = ""
		}
	}
	invoker := New(capabilities)
	invoker.Responses[protectionhelper.OperationNFTApply] = protectionhelper.Response{OK: true}
	request := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: protectionhelper.Correlation{OperationID: "nft", InstanceID: "test", LockRevision: 1}, Operation: protectionhelper.OperationNFTApply, NFTApply: &protectionhelper.NFTApplyRequest{ExpectedRevision: strings.Repeat("a", 64), ExpectedSHA256: strings.Repeat("b", 64)}}
	response, _, err := invoker.Invoke(context.Background(), request)
	if err != nil || !response.OK || response.NFT == nil || response.NFT.AppliedRevision != request.NFTApply.ExpectedRevision || response.NFT.CandidateSHA256 != request.NFTApply.ExpectedSHA256 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}
