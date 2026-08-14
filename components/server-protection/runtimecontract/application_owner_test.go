package runtimecontract

import (
	"strings"
	"testing"
)

func TestApplicationOwnerBindsInstalledRuntimeWithoutLabState(t *testing.T) {
	input := ApplicationOwnerInput{
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64),
		ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64),
		ServiceIdentity: "solovey-ui", SystemdUnit: "solovey-ui.service",
		ServiceFragmentPath: "/usr/local/lib/solovey-ui/systemd/solovey-ui-native-hardened.service",
		ServiceUnitSHA256:   strings.Repeat("4", 64), ServiceControlGroup: "/system.slice/solovey-ui.service",
		ExecutablePath:   "/usr/local/solovey-ui/releases/installer-0011223344556677/solovey-ui",
		ExecutableSHA256: strings.Repeat("5", 64), ProcessUID: 997, ProcessGID: 997,
	}
	contract, err := ApplicationOwner(input)
	if err != nil {
		t.Fatal(err)
	}
	if contract.InstanceID != input.InstanceID || contract.ExecutablePath != input.ExecutablePath ||
		contract.RuntimeRootBindingRevision == "" || contract.RuntimeRootContractRevision == "" {
		t.Fatalf("application owner contract is incomplete: %#v", contract)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
}
