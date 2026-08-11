package fronting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type fakeProbe struct {
	result ProbeResult
	err    error
	calls  int
	path   string
	args   []string
}

func (f *fakeProbe) Run(_ context.Context, path string, args []string, _, _ int) (ProbeResult, error) {
	f.calls++
	f.path, f.args = path, append([]string(nil), args...)
	return f.result, f.err
}

func TestNginxCapabilityFailsClosed(t *testing.T) {
	binary := fakeBinary(t, "nginx")
	valid := "nginx version: nginx/1.25.3\nbuilt with OpenSSL\nconfigure arguments: --conf-path=/etc/nginx/nginx.conf --with-stream --with-stream_ssl_preread_module\n"
	cases := []struct {
		name   string
		config func(NginxConfig) NginxConfig
		probe  fakeProbe
		want   DetectionState
	}{
		{"stderr", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, StateSupported},
		{"not found", func(c NginxConfig) NginxConfig {
			c.CandidatePaths = []string{filepath.Join(t.TempDir(), "missing")}
			return c
		}, fakeProbe{}, StateNotFound},
		{"timeout", func(c NginxConfig) NginxConfig { return c }, fakeProbe{err: context.DeadlineExceeded}, StateTimeout},
		{"oversized", func(c NginxConfig) NginxConfig { return c }, fakeProbe{err: ErrOutputLimit}, StateOversizedOutput},
		{"malformed", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("not nginx"), ExitCode: 0}}, StateMalformedOutput},
		{"version unknown", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("configure arguments: --with-stream"), ExitCode: 0}}, StateMalformedOutput},
		{"missing stream", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments:"), ExitCode: 0}}, StateMissingStream},
		{"missing preread", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream"), ExitCode: 0}}, StateMissingSSLPreread},
		{"dynamic unresolved", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream=dynamic --with-stream_ssl_preread_module=dynamic"), ExitCode: 0}}, StateDynamicModuleUnresolved},
		{"external", func(c NginxConfig) NginxConfig { c.ExternalManaged = true; return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, StateExternalManaged},
		{"root unknown", func(c NginxConfig) NginxConfig { c.ConfigRoot = ""; return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream --with-stream_ssl_preread_module"), ExitCode: 0}}, StateConfigRootUnknown},
		{"include not controlled", func(c NginxConfig) NginxConfig { c.ControlledInclude = ""; return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, StateIncludeNotControlled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := test.config(NginxConfig{Platform: "linux", CandidatePaths: []string{binary}, ConfigRoot: "/etc/nginx", ManagedRoot: "/runtime/fronting", ControlledInclude: "/etc/nginx/solovey.conf"})
			adapter := &NginxAdapter{Config: config, Runner: &test.probe}
			report := adapter.Capability(context.Background())
			if report.State != test.want || report.Supported != (test.want == StateSupported) {
				t.Fatalf("report = %#v", report)
			}
			if test.want == StateSupported && (test.probe.calls != 1 || strings.Join(test.probe.args, " ") != "-V" || report.Validate != AvailabilityUnsupported || report.Reload != AvailabilityUnsupported) {
				t.Fatalf("unexpected probe/capability: %#v %#v", test.probe, report)
			}
		})
	}
}

func TestNginxDetectionMultipleAndSymlink(t *testing.T) {
	first, second := fakeBinary(t, "nginx-a"), fakeBinary(t, "nginx-b")
	adapter := &NginxAdapter{Config: NginxConfig{Platform: "linux", CandidatePaths: []string{first, second}}, Runner: &fakeProbe{}}
	if got := adapter.Capability(context.Background()).State; got != StateMultipleBinaries {
		t.Fatalf("state = %s", got)
	}
	link := filepath.Join(t.TempDir(), "nginx-link")
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report := (&NginxAdapter{Config: NginxConfig{Platform: "linux", CandidatePaths: []string{link}, ConfigRoot: "/etc/nginx", ControlledInclude: "/etc/nginx/solovey.conf"}, Runner: &fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25\nconfigure arguments: --with-stream --with-stream_ssl_preread_module"), ExitCode: 0}}}).Capability(context.Background())
	if report.Binary.TargetPath != first || report.State != StateSupported {
		t.Fatalf("symlink report = %#v", report)
	}
}

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

func fakeBinary(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func resource(id, listen string, port int, fallback hostresources.CapabilityValue) hostresources.ProtectableResource {
	return hostresources.ProtectableResource{ID: id, Owner: "fixture", Fingerprint: id + "-revision", Listen: listen, Port: port, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, CanServeFallback: fallback}}
}
