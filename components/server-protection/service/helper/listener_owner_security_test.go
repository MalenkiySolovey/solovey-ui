package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

func TestFirewallBaselineSecurityCases33Through40(t *testing.T) {
	root := testManagedRoot(t)
	t.Run("33_arbitrary_PID_rejected", func(t *testing.T) {
		assertUnknownOwnerFieldRejected(t, validListenerOwnerRequest(), "expected_pid", 42)
	})
	t.Run("34_arbitrary_proc_or_path_rejected", func(t *testing.T) {
		assertUnknownOwnerFieldRejected(t, validListenerOwnerRequest(), "proc_path", "/proc/1/fd")
	})
	t.Run("35_shell_and_raw_command_unavailable", func(t *testing.T) {
		request := validListenerOwnerRequest()
		request.Operation = Operation("shell.execute")
		if err := request.Validate(root); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
			t.Fatalf("raw command escaped the allowlist: %v", err)
		}
	})
	t.Run("36_symlink_and_path_arguments_rejected", func(t *testing.T) {
		assertUnknownOwnerFieldRejected(t, validListenerOwnerRequest(), "path", "../../etc/shadow")
	})
	t.Run("37_process_scan_is_bounded", func(t *testing.T) {
		data, err := os.ReadFile("listener_owner_linux.go")
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "Readdirnames(4097)") || !strings.Contains(text, "len(names) > 4096") || strings.Contains(text, `os.ReadDir(fmt.Sprintf("/proc/%d/fd"`) {
			t.Fatal("listener owner fd discovery is not explicitly bounded")
		}
	})
	t.Run("38_diagnostics_are_secret_free", func(t *testing.T) {
		reasons := normalizedOwnerReasons([]string{"password=do-not-emit", "listener_unobserved"})
		encoded, _ := json.Marshal(reasons)
		for _, forbidden := range []string{"password", "do-not-emit", "secret", "cookie="} {
			if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
				t.Fatalf("diagnostics disclosed %q: %s", forbidden, encoded)
			}
		}
	})
	t.Run("39_QGA_cannot_be_owner_or_recovery_evidence", func(t *testing.T) {
		assertUnknownOwnerFieldRejected(t, validListenerOwnerRequest(), "qga_evidence", true)
	})
	t.Run("40_owner_observation_performs_no_nft_mutation", func(t *testing.T) {
		nft := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}}
		owner := &firewallBaselineOwnerExecutorFake{}
		engine := ContractEngine{root: root, executor: nft, listenerOwnerExecutor: owner}
		response := engine.Handle(validListenerOwnerRequest())
		if !response.OK || owner.calls != 1 || nft.checks != 0 || nft.applies != 0 {
			t.Fatalf("read-only owner observation crossed into nft: response=%#v checks=%d applies=%d", response, nft.checks, nft.applies)
		}
	})
}

func TestListenerOwnerObservationHasExplicitBoundedHashWindow(t *testing.T) {
	if got := timeoutFor(OperationListenerOwnerObserve); got != 60*time.Second {
		t.Fatalf("listener owner timeout = %s, want 60s", got)
	}
	if got := timeoutFor(OperationCapabilities); got >= timeoutFor(OperationListenerOwnerObserve) {
		t.Fatalf("ordinary read-only timeout %s was widened with owner hashing", got)
	}
}

type firewallBaselineOwnerExecutorFake struct{ calls int }

func (f *firewallBaselineOwnerExecutorFake) Detect(context.Context) ListenerOwnerSupport {
	return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Available: true, ContractRevision: strings.Repeat("a", 64), ObserverRevision: strings.Repeat("b", 64)}
}

func (f *firewallBaselineOwnerExecutorFake) Observe(context.Context, ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	f.calls++
	result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}, ReasonCodes: []string{"listener_unobserved"}}
	sealListenerOwnerResult(result)
	return result, nil
}

func validListenerOwnerRequest() Request {
	return Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "owner-observe", InstanceID: "00112233-4455-4677-8899-aabbccddeeff"}, Operation: OperationListenerOwnerObserve, ListenerOwnerObserve: &ListenerOwnerObserveRequest{ResourceID: "core:panel:web", Network: "tcp", ConfiguredMode: "wildcard", ConfiguredAddress: "*", Port: 443, ExpectedInstanceID: "00112233-4455-4677-8899-aabbccddeeff", ExpectedSourceRevision: "src-" + strings.Repeat("2", 64), ExpectedArtifactRevision: "art-" + strings.Repeat("3", 64), ExpectedDeploymentID: "dep-" + strings.Repeat("4", 64), ExpectedOwnerContractRevision: strings.Repeat("a", 64), ExpectedRuntimeRootBindingRevision: strings.Repeat("5", 64), ExpectedResourceOwnerRevision: strings.Repeat("b", 64), ExpectedConfigurationRevision: strings.Repeat("c", 64)}}
}

func assertUnknownOwnerFieldRejected(t *testing.T, request Request, name string, value any) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	payload := document["listener_owner_observe"].(map[string]any)
	payload[name] = value
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest(bytes.NewReader(data)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown listener owner field %q was accepted: %v", name, err)
	}
}
