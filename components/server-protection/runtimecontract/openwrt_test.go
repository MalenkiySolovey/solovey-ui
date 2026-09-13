package runtimecontract

import (
	"strings"
	"testing"
)

func TestOpenWrtRuntimeAndApplicationOwnerAreVersionedSeparately(t *testing.T) {
	runtime := OpenWrtInstalled()
	if err := runtime.Validate(); err != nil {
		t.Fatal(err)
	}
	if runtime.RuntimeRoot == Installed().RuntimeRoot {
		t.Fatal("OpenWrt runtime root reused the systemd install-root contract")
	}
	contract, err := OpenWrtApplicationOwner(OpenWrtApplicationOwnerInput{
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64),
		ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64),
		ServiceIdentity: "solovey-ui", ProcdService: "solovey-ui", ProcdInstance: "panel",
		ExecutablePath: "/usr/lib/solovey-ui/releases/current/solovey-ui", ExecutableSHA256: strings.Repeat("4", 64), ProcessUID: 997, ProcessGID: 997,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	if contract.Schema == "" || contract.RuntimeRootBindingRevision == "" || contract.ProcdInstance != "panel" {
		t.Fatalf("OpenWrt application owner contract = %#v", contract)
	}
}
