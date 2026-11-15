package backendfronting

import (
	"context"
	"errors"
	fronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

type versionReaderV2 struct {
	observation fronting.NginxVersionObservationV2
	err         error
	mutate      bool
}

func (r versionReaderV2) ReadNginxVersion(_ context.Context, path string) (fronting.NginxVersionObservationV2, error) {
	if r.mutate {
		_ = os.WriteFile(path, []byte("changed executable"), 0600)
	}
	return r.observation, r.err
}

func readyRuntimeIdentityV2(t *testing.T, now time.Time, arguments []string, loaded []string) fronting.NginxRuntimeIdentityV2 {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "nginx")
	if err := os.WriteFile(executable, []byte("fixture executable"), 0700); err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(root, "managed")
	if err := os.MkdirAll(managedRoot, 0700); err != nil {
		t.Fatal(err)
	}
	controlled := filepath.Join(managedRoot, "loader.conf")
	if err := os.WriteFile(controlled, []byte("managed loader"), 0600); err != nil {
		t.Fatal(err)
	}
	supported := func(label string) fronting.NginxMethodCapabilityV2 {
		return fronting.NginxMethodCapabilityV2{Availability: fronting.CapabilitySupportedV2, Revision: fronting.NginxRuntimeFactRevisionV2(label)}
	}
	config := fronting.NginxRuntimeInspectionConfigV2{
		CandidatePaths: []string{executable}, AllowedExecutableRoots: []string{root}, ManagedRootPath: managedRoot,
		ControlledConfigPath: controlled, InstallationClass: fronting.NginxInstallationManaged, LoadedDynamicModules: loaded,
		ValidationMethod: supported("validation"), ReloadMethod: supported("reload"), ActiveVerification: supported("active"),
		ProcessVerification: supported("process"), ListenerVerification: supported("listener"),
		ProxyProtocolReceive: supported("proxy-receive"), ProxyProtocolEmit: supported("proxy-emit"),
		MasterProcessIdentityRevision: fronting.NginxRuntimeFactRevisionV2("master"), WorkerSetIdentityRevision: fronting.NginxRuntimeFactRevisionV2("workers"),
		ActiveManagedRevision: fronting.NginxRuntimeFactRevisionV2("active"), HelperProtocolVersion: 1, HelperVersion: "1.5.1",
		HelperContractVersion: "1.5", HelperContractRevision: fronting.NginxRuntimeFactRevisionV2("helper"),
		ManagementExclusionsRevision: fronting.NginxRuntimeFactRevisionV2("management"), ObservedAt: now, ExpiresAt: now.Add(2 * time.Minute),
	}
	identity, err := (NginxRuntimeInspectorV2{Config: config, Reader: versionReaderV2{observation: fronting.NginxVersionObservationV2{Version: "nginx/1.27.4", ConfigureArguments: arguments}}}).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestRuntimeIdentityV2FailClosedMatrix(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	root := t.TempDir()
	base := fronting.NginxRuntimeInspectionConfigV2{AllowedExecutableRoots: []string{root}, InstallationClass: fronting.NginxInstallationManaged, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	identity, err := (NginxRuntimeInspectorV2{Config: base}).Inspect(context.Background())
	if err != nil || identity.State != fronting.NginxNotInstalled {
		t.Fatalf("absent identity=%#v err=%v", identity, err)
	}
	first, second := filepath.Join(root, "nginx-a"), filepath.Join(root, "nginx-b")
	_ = os.WriteFile(first, []byte("a"), 0700)
	_ = os.WriteFile(second, []byte("b"), 0700)
	ambiguous := base
	ambiguous.CandidatePaths = []string{first, second}
	identity, _ = (NginxRuntimeInspectorV2{Config: ambiguous, Reader: versionReaderV2{}}).Inspect(context.Background())
	if identity.State != fronting.NginxIdentityUnknown || !containsReasonV2(identity.ReasonCodes, "nginx_executable_ambiguous") {
		t.Fatalf("ambiguous identity=%#v", identity)
	}
	nonRegular := base
	nonRegular.CandidatePaths = []string{root}
	identity, _ = (NginxRuntimeInspectorV2{Config: nonRegular}).Inspect(context.Background())
	if !containsReasonV2(identity.ReasonCodes, "nginx_executable_non_regular") {
		t.Fatalf("non-regular identity=%#v", identity)
	}
	escaped := base
	outside := filepath.Join(t.TempDir(), "outside-nginx")
	if err := os.WriteFile(outside, []byte("outside"), 0700); err != nil {
		t.Fatal(err)
	}
	escaped.CandidatePaths = []string{outside}
	identity, _ = (NginxRuntimeInspectorV2{Config: escaped}).Inspect(context.Background())
	if !containsReasonV2(identity.ReasonCodes, "nginx_executable_path_escape") {
		t.Fatalf("path escape identity=%#v", identity)
	}
	malformed := ambiguous
	malformed.CandidatePaths = []string{first}
	identity, _ = (NginxRuntimeInspectorV2{Config: malformed, Reader: versionReaderV2{err: errors.New("malformed")}}).Inspect(context.Background())
	if !containsReasonV2(identity.ReasonCodes, "nginx_version_unparseable") {
		t.Fatalf("malformed identity=%#v", identity)
	}
	drift := malformed
	identity, _ = (NginxRuntimeInspectorV2{Config: drift, Reader: versionReaderV2{mutate: true, observation: fronting.NginxVersionObservationV2{Version: "1.27.4"}}}).Inspect(context.Background())
	if !containsReasonV2(identity.ReasonCodes, "nginx_executable_identity_drift") {
		t.Fatalf("drift identity=%#v", identity)
	}
}

func TestRuntimeIdentityV2ManagedContourRevisionSeparatesResolvedRoots(t *testing.T) {
	stamp := time.Unix(1_700_000_000, 0).UTC()
	makeContour := func() (string, string) {
		root := t.TempDir()
		config := filepath.Join(root, "loader.conf")
		if err := os.WriteFile(config, []byte("same managed loader"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(config, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(root, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return root, config
	}
	firstRoot, firstConfig := makeContour()
	secondRoot, secondConfig := makeContour()
	firstRootRevision, firstOwnershipRevision, firstOK := inspectManagedContourV2(firstRoot, firstConfig)
	secondRootRevision, secondOwnershipRevision, secondOK := inspectManagedContourV2(secondRoot, secondConfig)
	if !firstOK || !secondOK || firstRootRevision == secondRootRevision || firstOwnershipRevision == secondOwnershipRevision {
		t.Fatalf("distinct resolved managed contours collided: first=%s/%s second=%s/%s", firstRootRevision, firstOwnershipRevision, secondRootRevision, secondOwnershipRevision)
	}
}

func TestRuntimeIdentityV2DynamicModulesAndSymlinkEscapeFailClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	dynamicLoaded := readyRuntimeIdentityV2(t, now, []string{"--with-stream=dynamic"}, []string{"stream"})
	if dynamicLoaded.Stream.State != fronting.NginxModuleDynamicLoaded || dynamicLoaded.State != fronting.NginxManagedEngineReady {
		t.Fatalf("loaded dynamic module state=%#v", dynamicLoaded)
	}
	dynamicNotLoaded := readyRuntimeIdentityV2(t, now, []string{"--with-stream=dynamic", "--with-stream_ssl_preread_module=dynamic"}, []string{})
	if dynamicNotLoaded.Stream.State != fronting.NginxModuleDynamicNotLoaded || dynamicNotLoaded.SSLPreread.State != fronting.NginxModuleDynamicNotLoaded ||
		dynamicNotLoaded.State != fronting.NginxIdentityUnknown {
		t.Fatalf("dynamic module state=%#v", dynamicNotLoaded)
	}
	dynamicUnknown := readyRuntimeIdentityV2(t, now, []string{"--with-stream=dynamic"}, nil)
	if dynamicUnknown.Stream.State != fronting.NginxModuleUnknown || dynamicUnknown.Stream.Effective != fronting.CapabilityUnknownV2 ||
		dynamicUnknown.State != fronting.NginxIdentityUnknown {
		t.Fatalf("unknown dynamic module state=%#v", dynamicUnknown)
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "nginx")
	if err := os.WriteFile(outside, []byte("outside"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "nginx")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	config := fronting.NginxRuntimeInspectionConfigV2{CandidatePaths: []string{link}, AllowedExecutableRoots: []string{root},
		InstallationClass: fronting.NginxInstallationManaged, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	identity, err := (NginxRuntimeInspectorV2{Config: config}).Inspect(context.Background())
	if err != nil || !containsReasonV2(identity.ReasonCodes, "nginx_executable_path_escape") {
		t.Fatalf("symlink escape identity=%#v err=%v", identity, err)
	}
}

func containsReasonV2(values []string, value string) bool { return slices.Contains(values, value) }
