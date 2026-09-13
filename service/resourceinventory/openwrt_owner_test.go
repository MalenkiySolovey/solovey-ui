package resourceinventory

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

func TestApplicationOwnerProjectionConsumesOneSemanticContract(t *testing.T) {
	openwrt, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(openwrt)
	if err != nil || !expected.Valid() || expected.ServiceIdentity != "solovey-ui-panel" {
		t.Fatalf("OpenWrt semantic owner projection = %#v, %v", expected, err)
	}
}
