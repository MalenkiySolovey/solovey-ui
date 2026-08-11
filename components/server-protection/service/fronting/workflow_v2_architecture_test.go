package fronting

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkflowV2ReusesRestrictedEngineAndHasNoPublicOrDynamicDestinationSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	workflow, err := os.ReadFile(filepath.Join(directory, "workflow_v2.go"))
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := os.ReadFile(filepath.Join(directory, "fixed_l4_v2.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow) + string(renderer)
	for _, required := range []string{"OperationNginxValidate", "OperationNginxInstall", "OperationNginxSwitch", "OperationNginxReload", "OperationNginxVerify", "OperationNginxRestore", "Manager.Acquire", "MarkMutation", "VerifyRevision"} {
		if !strings.Contains(text, required) {
			t.Fatalf("v2 bridge no longer calls existing engine contract %s", required)
		}
	}
	for _, forbidden := range []string{"os/exec", "exec.Command", "syscall", "net.Dial(", "net.Lookup", "Resolver", "RawConfig", "RawSnippet", "SNI_PREREAD_FRONTING\nupstream", "ssl_preread on"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("v2 bridge gained forbidden execution/destination surface %q", forbidden)
		}
	}
	api, err := os.ReadFile(filepath.Join(directory, "..", "..", "api", "fronting.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(api), ".PrepareV2(") || strings.Contains(string(api), ".ApplyV2(") {
		t.Fatal("the low-level Slice 2 workflow was exposed through the semantic API")
	}
	for _, required := range []string{"frontingSemanticService", "FrontingStrategyPlanV2", "frontingApplyConfigured"} {
		if !strings.Contains(string(api), required) {
			t.Fatalf("reviewed semantic fronting API lost %q", required)
		}
	}
}

func TestSNIPrereadRendererHasNoSecondEngineOrDynamicActionSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	files := []string{"sni_preread_v2.go", "workflow_candidate_v2.go", "target_authority_v2.go", "sni_health_v2.go"}
	var source strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		source.Write(data)
	}
	text := source.String()
	for _, required := range []string{"ssl_preread on;", "RenderSNIPrereadCandidateV2", "renderWorkflowCandidateV2", "EndpointLeaseProviderV1", "ProviderV2", "SNIPrereadHealthCheckV2"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SNI workflow integration lost %q", required)
		}
	}
	for _, forbidden := range []string{"os/exec", "exec.Command", "syscall", "net.Dial(", "net.Lookup", "http.Listen", "AppliedActionV1", "ActionMap"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("SNI integration gained a second engine, action map, or forbidden dependency %q", forbidden)
		}
	}
}
