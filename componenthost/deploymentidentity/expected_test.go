package deploymentidentity

import "testing"

func TestInstalledOwnerProofsProjectToOneSemanticContract(t *testing.T) {
	const (
		instance = "00112233-4455-4677-8899-aabbccddeeff"
		source   = "src-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		artifact = "art-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		deploy   = "dep-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		runtime  = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		binding  = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		exeSHA   = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	)
	systemd, err := NewSystemdV1(instance, source, artifact, deploy, runtime, binding, "solovey-ui", "solovey-ui.service",
		"/etc/systemd/system/solovey-ui.service", exeSHA, "/system.slice/solovey-ui.service", "/usr/lib/solovey-ui/solovey-ui", exeSHA, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	procd, err := NewProcdV1(instance, source, artifact, deploy, runtime, binding, "solovey-ui", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", exeSHA, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	wantSystemd, err := ExpectedSystemdApplicationOwner(systemd)
	if err != nil {
		t.Fatal(err)
	}
	wantProcd, err := ExpectedProcdApplicationOwner(procd)
	if err != nil {
		t.Fatal(err)
	}
	wantSystemd.ContractRevision, wantProcd.ContractRevision = "", ""
	if wantSystemd != wantProcd {
		t.Fatalf("semantic projections differ:\nSystemd: %#v\nprocd: %#v", wantSystemd, wantProcd)
	}
}
