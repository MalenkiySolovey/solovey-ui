package backendfronting

import (
	"context"
	fronting "github.com/MalenkiySolovey/solovey-component-server-protection/service/fronting"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		want   fronting.DetectionState
	}{
		{"stderr", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, fronting.StateSupported},
		{"not found", func(c NginxConfig) NginxConfig {
			c.CandidatePaths = []string{filepath.Join(t.TempDir(), "missing")}
			return c
		}, fakeProbe{}, fronting.StateNotFound},
		{"timeout", func(c NginxConfig) NginxConfig { return c }, fakeProbe{err: context.DeadlineExceeded}, fronting.StateTimeout},
		{"oversized", func(c NginxConfig) NginxConfig { return c }, fakeProbe{err: ErrOutputLimit}, fronting.StateOversizedOutput},
		{"malformed", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("not nginx"), ExitCode: 0}}, fronting.StateMalformedOutput},
		{"version unknown", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("configure arguments: --with-stream"), ExitCode: 0}}, fronting.StateMalformedOutput},
		{"missing stream", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments:"), ExitCode: 0}}, fronting.StateMissingStream},
		{"missing preread", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream"), ExitCode: 0}}, fronting.StateMissingSSLPreread},
		{"dynamic unresolved", func(c NginxConfig) NginxConfig { return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream=dynamic --with-stream_ssl_preread_module=dynamic"), ExitCode: 0}}, fronting.StateDynamicModuleUnresolved},
		{"external", func(c NginxConfig) NginxConfig { c.ExternalManaged = true; return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, fronting.StateExternalManaged},
		{"root unknown", func(c NginxConfig) NginxConfig { c.ConfigRoot = ""; return c }, fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25.3\nconfigure arguments: --with-stream --with-stream_ssl_preread_module"), ExitCode: 0}}, fronting.StateConfigRootUnknown},
		{"include not controlled", func(c NginxConfig) NginxConfig { c.ControlledInclude = ""; return c }, fakeProbe{result: ProbeResult{Stderr: []byte(valid), ExitCode: 0}}, fronting.StateIncludeNotControlled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := test.config(NginxConfig{ProbeCapability: fronting.AvailabilitySupported, CandidatePaths: []string{binary}, ConfigRoot: "/etc/nginx", ManagedRoot: "/runtime/fronting", ControlledInclude: "/etc/nginx/solovey.conf"})
			adapter := &PreviewInspector{Config: config, Runner: &test.probe}
			report := adapter.Capability(context.Background())
			if report.State != test.want || report.Supported != (test.want == fronting.StateSupported) {
				t.Fatalf("report = %#v", report)
			}
			if test.want == fronting.StateSupported && (test.probe.calls != 1 || strings.Join(test.probe.args, " ") != "-V" || report.Validate != fronting.AvailabilityUnsupported || report.Reload != fronting.AvailabilityUnsupported) {
				t.Fatalf("unexpected probe/capability: %#v %#v", test.probe, report)
			}
		})
	}
}

func TestNginxDetectionMultipleAndSymlink(t *testing.T) {
	first, second := fakeBinary(t, "nginx-a"), fakeBinary(t, "nginx-b")
	adapter := &PreviewInspector{Config: NginxConfig{ProbeCapability: fronting.AvailabilitySupported, CandidatePaths: []string{first, second}}, Runner: &fakeProbe{}}
	if got := adapter.Capability(context.Background()).State; got != fronting.StateMultipleBinaries {
		t.Fatalf("state = %s", got)
	}
	link := filepath.Join(t.TempDir(), "nginx-link")
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report := (&PreviewInspector{Config: NginxConfig{ProbeCapability: fronting.AvailabilitySupported, CandidatePaths: []string{link}, ConfigRoot: "/etc/nginx", ControlledInclude: "/etc/nginx/solovey.conf"}, Runner: &fakeProbe{result: ProbeResult{Stderr: []byte("nginx version: nginx/1.25\nconfigure arguments: --with-stream --with-stream_ssl_preread_module"), ExitCode: 0}}}).Capability(context.Background())
	firstInfo, firstErr := os.Stat(first)
	targetInfo, targetErr := os.Stat(report.Binary.TargetPath)
	if firstErr != nil || targetErr != nil || !os.SameFile(firstInfo, targetInfo) || report.State != fronting.StateSupported {
		t.Fatalf("symlink report = %#v", report)
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
