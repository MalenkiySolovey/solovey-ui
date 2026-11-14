package helpercontract

import (
	"strings"
	"testing"
)

func TestUnknownCapabilitiesDenyExecution(t *testing.T) {
	if CapabilityAvailable(nil, OperationNFTApply) || SSHRecoveryCapabilityAvailable(nil) {
		t.Fatal("missing capability evidence admitted execution")
	}
	result := DefaultCapabilities()
	if err := ValidateCapabilities(result); err != nil {
		t.Fatal(err)
	}
	for _, capability := range result.Capabilities {
		if capability.Available != (capability.Operation == OperationCapabilities) {
			t.Fatalf("unknown platform admitted %s", capability.Operation)
		}
	}
	if CapabilityAvailable(result, Operation("shell.execute")) || SSHRecoveryCapabilityAvailable(result) {
		t.Fatal("unknown operation or owner was accepted")
	}
	result.NFT.Available = true
	if ValidateCapabilities(result) == nil {
		t.Fatal("changed capability evidence retained a valid revision")
	}
}

func TestDeclaredEnvelopeRejectsUnboundedAuthority(t *testing.T) {
	valid := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "op", InstanceID: "instance"}, Operation: OperationCapabilities, Capabilities: &CapabilitiesRequest{}}
	if err := valid.ValidateShape(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Request){
		"arbitrary execution":   func(r *Request) { r.Operation = "shell.execute" },
		"unknown protocol":      func(r *Request) { r.ProtocolVersion++ },
		"unbounded correlation": func(r *Request) { r.Correlation.OperationID = strings.Repeat("x", 129) },
		"unfenced mutation":     func(r *Request) { r.Operation, r.Capabilities, r.NFTApply = OperationNFTApply, nil, &NFTApplyRequest{} },
		"mismatched payload":    func(r *Request) { r.Operation = OperationNFTObserve },
		"multiple payloads":     func(r *Request) { r.NFTObserve = &NFTObserveRequest{} },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if request.ValidateShape() == nil {
				t.Fatal("invalid declaration admitted")
			}
		})
	}
	for _, payload := range []string{
		`{"command":"arbitrary"}`,
		`{} {}`,
		strings.Repeat(" ", MaxRequestBytes+1),
	} {
		if _, err := DecodeRequest(strings.NewReader(payload)); err == nil {
			t.Fatal("unbounded or unknown transport data admitted")
		}
	}
}
