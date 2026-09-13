package resources

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

func TestOpenWrtApplicationOwnerProjectionIsVersionedAndFingerprinted(t *testing.T) {
	contract, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/releases/current/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(contract)
	if err != nil || !expected.Valid() || expected.ServiceIdentity != "solovey-ui" {
		t.Fatalf("OpenWrt application owner projection = %#v, %v", expected, err)
	}
	resource := ProtectableResource{Kind: "inbound", Owner: "core", Protocol: "stream", Listen: "127.0.0.1", Port: 443,
		Capabilities: ProtectableResourceCapabilities{ExpectedApplicationOwner: expected}}
	first := Fingerprint(resource)
	resource.Capabilities.ExpectedApplicationOwner.DeploymentID = "dep-" + strings.Repeat("9", 64)
	if first == Fingerprint(resource) {
		t.Fatal("expected application owner projection did not bind the resource fingerprint")
	}
}
