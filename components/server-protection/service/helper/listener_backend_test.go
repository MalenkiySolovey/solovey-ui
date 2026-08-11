package helper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortHandoffListenerProbeUsesFakeExecutorInNormalCI(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := NewManagedRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &FakeListenerExecutor{Results: []ListenerProbeResult{{Reachable: true, OwnerMatched: true, OwnerClass: ListenerOwnerPanel, Detail: "fake_exact_listener"}}}
	engine := newContractEngineWithExecutors(root, nil, fake)
	request := Request{
		ProtocolVersion: ProtocolVersion,
		Correlation:     Correlation{OperationID: "operation-fake", InstanceID: "fake-ci", LockRevision: 3},
		Operation:       OperationListenerProbe,
		ListenerProbe:   &ListenerProbeRequest{Purpose: ProbePortHandoff, Network: "tcp", Address: "203.0.113.7", Port: 443, ExpectedOwner: ListenerOwnerPanel, ExpectedPID: 42},
	}
	response := engine.Handle(request)
	if !response.OK || response.ListenerProbe == nil || !response.ListenerProbe.Reachable {
		t.Fatalf("response=%#v", response)
	}
	if len(fake.Requests) != 1 || fake.Requests[0] != *request.ListenerProbe {
		t.Fatalf("requests=%#v", fake.Requests)
	}
}

func TestPortHandoffListenerProbeRejectsWildcardAndUDP(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := NewManagedRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	base := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-fake", InstanceID: "fake-ci", LockRevision: 3}, Operation: OperationListenerProbe}
	for _, probe := range []ListenerProbeRequest{
		{Purpose: ProbePortHandoff, Network: "tcp", Address: "0.0.0.0", Port: 443, ExpectedOwner: ListenerOwnerPanel, ExpectedPID: 42},
		{Purpose: ProbePortHandoff, Network: "udp", Address: "127.0.0.1", Port: 443, ExpectedOwner: ListenerOwnerPanel, ExpectedPID: 42},
		{Purpose: ProbePortHandoff, Network: "tcp", Address: "127.0.0.1", Port: 443, ExpectedOwner: ListenerOwnerPanel},
	} {
		request := base
		request.ListenerProbe = &probe
		if err := request.Validate(root); err == nil {
			t.Fatalf("accepted unsafe probe %#v", probe)
		}
	}
}
