package fronting

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

func TestPreviewIsDeterministicAndRefusesUnsafeRoutes(t *testing.T) {
	resources := []hostresources.ProtectableResource{resource("decoy", "127.0.0.1", 9443, hostresources.CapabilityYes), resource("inbound", "127.0.0.1", 8443, hostresources.CapabilityNo)}
	input := PreviewInput{Resources: resources, FallbackResourceID: "decoy", Routes: []RouteInput{{ResourceID: "inbound", ResourceRevision: "inbound-revision", SNI: []string{"b.example", "a.example"}, ALPN: []string{"h2"}, Listen: ListenSpec{Address: "0.0.0.0", Port: 443}}}}
	first, err := GeneratePreview(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Routes[0].SNI = []string{"a.example", "b.example"}
	second, err := GeneratePreview(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.DesiredRevision != second.DesiredRevision || first.GeneratedSHA256 != second.GeneratedSHA256 || first.GeneratedConfig != second.GeneratedConfig {
		t.Fatalf("preview is not deterministic: %#v %#v", first, second)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "fronting.golden.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if first.GeneratedConfig != string(golden) {
		t.Fatalf("generated candidate differs from golden:\n%s", first.GeneratedConfig)
	}
	if strings.Contains(first.GeneratedConfig, "proxy_pass $ssl_preread_server_name") || !strings.Contains(first.GeneratedConfig, "proxy_pass $solovey_static_upstream") {
		t.Fatalf("unsafe generated config: %s", first.GeneratedConfig)
	}
	unsafeInput := input
	unsafeInput.Routes = append(unsafeInput.Routes, RouteInput{ResourceID: "inbound", ResourceRevision: "inbound-revision", SNI: []string{"a.example"}, Listen: ListenSpec{Address: "127.0.0.1", Port: 443}})
	if _, err := GeneratePreview(unsafeInput); !errors.Is(err, ErrUnsafeRoute) {
		t.Fatalf("duplicate SNI = %v", err)
	}
	unsafeInput = input
	unsafeInput.Routes[0].ResourceRevision = "stale"
	if _, err := GeneratePreview(unsafeInput); !errors.Is(err, ErrUnsafeRoute) {
		t.Fatalf("stale target = %v", err)
	}
	unsafeInput = input
	unsafeInput.Resources = append([]hostresources.ProtectableResource(nil), resources...)
	unsafeInput.Resources[1].Listen, unsafeInput.Resources[1].Port = "203.0.113.8", 443
	if _, err := GeneratePreview(unsafeInput); !errors.Is(err, ErrUnsafeRoute) {
		t.Fatalf("external target = %v", err)
	}
}

func TestPreviewProxyAndStaleRevisionFailClosed(t *testing.T) {
	input := PreviewInput{CurrentRevision: "current", ExpectedCurrentRevision: "old"}
	if _, err := GeneratePreview(input); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision = %v", err)
	}
	input = PreviewInput{Resources: []hostresources.ProtectableResource{resource("target", "127.0.0.1", 8443, hostresources.CapabilityUnknown)}, Routes: []RouteInput{{ResourceID: "target", ResourceRevision: "target-revision", SNI: []string{"a.example"}, Listen: ListenSpec{Address: "127.0.0.1", Port: 443}, ProxyProtocol: true}}}
	if _, err := GeneratePreview(input); !errors.Is(err, ErrUnsafeRoute) {
		t.Fatalf("proxy mismatch = %v", err)
	}
}

func TestPreviewRejectsDirectiveShapedSNI(t *testing.T) {
	target := resource("target", "127.0.0.1", 8443, hostresources.CapabilityNo)
	injected := "panel.example\"\tsolovey_target_target;\n}\nserver\t{\nlisten\t9443;\nproxy_pass\tsolovey_target_target;\n}\nserver\t{\nlisten\t9444;\n#"
	input := PreviewInput{Resources: []hostresources.ProtectableResource{target}, Routes: []RouteInput{{
		ResourceID: target.ID, ResourceRevision: target.Fingerprint, SNI: []string{injected}, ALPN: []string{"h2"}, Listen: ListenSpec{Address: "0.0.0.0", Port: 443},
	}}}
	if preview, err := GeneratePreview(input); !errors.Is(err, ErrUnsafeRoute) {
		t.Fatalf("directive-shaped SNI was accepted: err=%v config=%q", err, preview.GeneratedConfig)
	}
}

func resource(id, listen string, port int, fallback hostresources.CapabilityValue) hostresources.ProtectableResource {
	return hostresources.ProtectableResource{ID: id, Owner: "fixture", Fingerprint: id + "-revision", Listen: listen, Port: port, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, CanServeFallback: fallback}}
}
