package fronting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type versionReaderV2 struct {
	observation NginxVersionObservationV2
	err         error
	mutate      bool
}

func (r versionReaderV2) ReadNginxVersion(_ context.Context, path string) (NginxVersionObservationV2, error) {
	if r.mutate {
		_ = os.WriteFile(path, []byte("changed executable"), 0600)
	}
	return r.observation, r.err
}

func readyRuntimeIdentityV2(t *testing.T, now time.Time, arguments []string, loaded []string) NginxRuntimeIdentityV2 {
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
	supported := func(label string) NginxMethodCapabilityV2 {
		return NginxMethodCapabilityV2{Availability: CapabilitySupportedV2, Revision: v2Revision(label)}
	}
	config := NginxRuntimeInspectionConfigV2{
		CandidatePaths: []string{executable}, AllowedExecutableRoots: []string{root}, ManagedRootPath: managedRoot,
		ControlledConfigPath: controlled, InstallationClass: NginxInstallationManaged, LoadedDynamicModules: loaded,
		ValidationMethod: supported("validation"), ReloadMethod: supported("reload"), ActiveVerification: supported("active"),
		ProcessVerification: supported("process"), ListenerVerification: supported("listener"),
		ProxyProtocolReceive: supported("proxy-receive"), ProxyProtocolEmit: supported("proxy-emit"),
		MasterProcessIdentityRevision: v2Revision("master"), WorkerSetIdentityRevision: v2Revision("workers"),
		ActiveManagedRevision: v2Revision("active"), HelperProtocolVersion: 1, HelperVersion: "1.5.1",
		HelperContractVersion: "1.5", HelperContractRevision: v2Revision("helper"),
		ManagementExclusionsRevision: v2Revision("management"), ObservedAt: now, ExpiresAt: now.Add(2 * time.Minute),
	}
	identity, err := (NginxRuntimeInspectorV2{Config: config, Reader: versionReaderV2{observation: NginxVersionObservationV2{Version: "nginx/1.27.4", ConfigureArguments: arguments}}}).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestRuntimeIdentityV2DeterministicModuleTruthAndRedaction(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	arguments := []string{"--with-stream", "--with-stream_ssl_preread_module=dynamic", "--with-stream_ssl_module", "--with-stream_realip_module"}
	first := readyRuntimeIdentityV2(t, now, arguments, []string{"ssl_preread"})
	second := finalizeRuntimeIdentity(first)
	normalizedFirst, okFirst := normalizeConfigureArgumentsV2(arguments)
	normalizedSecond, okSecond := normalizeConfigureArgumentsV2([]string{"--with-stream_realip_module", "--with-stream_ssl_module", "--with-stream", "--with-stream_ssl_preread_module=dynamic"})
	if first.State != NginxManagedEngineReady || first.Stream.State != NginxModuleBuiltIn || first.SSLPreread.State != NginxModuleDynamicLoaded ||
		first.StreamSSL.State != NginxModuleBuiltIn || first.StreamRealIP.State != NginxModuleBuiltIn || first.CanonicalRuntimeIdentityRevision != second.CanonicalRuntimeIdentityRevision ||
		!okFirst || !okSecond || v2Revision(normalizedFirst) != v2Revision(normalizedSecond) {
		t.Fatalf("identity mismatch: %#v %#v", first, second)
	}
	firstCapability := ResolveNginxStrategyCapabilityV2(first, StrategyL4OneToOne, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	refreshed := first
	refreshed.ObservedAt, refreshed.ExpiresAt = now.Add(time.Second).Unix(), now.Add(2*time.Minute+time.Second).Unix()
	refreshed = finalizeRuntimeIdentity(refreshed)
	refreshedCapability := ResolveNginxStrategyCapabilityV2(refreshed, StrategyL4OneToOne, hostresources.ProxyModeOff, CapabilitySupportedV2, now.Add(time.Second))
	if refreshed.Validate(now.Add(time.Second)) != nil || refreshed.CanonicalRuntimeIdentityRevision != first.CanonicalRuntimeIdentityRevision ||
		refreshedCapability.CapabilityRevision != firstCapability.CapabilityRevision {
		t.Fatalf("freshness refresh changed canonical revisions: runtime=%s/%s capability=%s/%s", first.CanonicalRuntimeIdentityRevision,
			refreshed.CanonicalRuntimeIdentityRevision, firstCapability.CapabilityRevision, refreshedCapability.CapabilityRevision)
	}
	encoded, _ := json.Marshal(first)
	for _, forbidden := range []string{t.TempDir(), "--with-stream", "configure arguments", "loader.conf", "nginx -V"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("runtime identity leaked %q: %s", forbidden, encoded)
		}
	}
	changed := first
	changed.ProxyProtocolEmit.Revision = v2Revision("changed")
	changed = finalizeRuntimeIdentity(changed)
	if changed.CanonicalRuntimeIdentityRevision == first.CanonicalRuntimeIdentityRevision {
		t.Fatal("safety-relevant method revision did not change identity")
	}
}

func TestFrontingSocketClaimRevisionIgnoresOnlyFreshnessWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	first, err := FinalizeFrontingSocketClaimV1(socketFixtureV1(now, "192.0.2.20", hostresources.AddressFamilyIPv4, false))
	if err != nil {
		t.Fatal(err)
	}
	refreshed := first
	refreshed.ObservedAt, refreshed.ExpiresAt = now.Add(time.Second).Unix(), now.Add(time.Minute+time.Second).Unix()
	refreshed, err = FinalizeFrontingSocketClaimV1(refreshed)
	if err != nil || refreshed.Validate(now.Add(time.Second)) != nil || refreshed.ClaimRevision != first.ClaimRevision {
		t.Fatalf("freshness refresh changed claim revision: first=%s refreshed=%s err=%v", first.ClaimRevision, refreshed.ClaimRevision, err)
	}
	changed := refreshed
	changed.TopologyOwnershipEligibilityRevision = v2Revision("changed-topology")
	changed, err = FinalizeFrontingSocketClaimV1(changed)
	if err != nil || changed.ClaimRevision == first.ClaimRevision {
		t.Fatalf("safety-relevant topology change retained claim revision: changed=%s err=%v", changed.ClaimRevision, err)
	}
}

func TestRuntimeIdentityV2FailClosedMatrix(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	root := t.TempDir()
	base := NginxRuntimeInspectionConfigV2{AllowedExecutableRoots: []string{root}, InstallationClass: NginxInstallationManaged, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	identity, err := (NginxRuntimeInspectorV2{Config: base}).Inspect(context.Background())
	if err != nil || identity.State != NginxNotInstalled {
		t.Fatalf("absent identity=%#v err=%v", identity, err)
	}
	first, second := filepath.Join(root, "nginx-a"), filepath.Join(root, "nginx-b")
	_ = os.WriteFile(first, []byte("a"), 0700)
	_ = os.WriteFile(second, []byte("b"), 0700)
	ambiguous := base
	ambiguous.CandidatePaths = []string{first, second}
	identity, _ = (NginxRuntimeInspectorV2{Config: ambiguous, Reader: versionReaderV2{}}).Inspect(context.Background())
	if identity.State != NginxIdentityUnknown || !containsReasonV2(identity.ReasonCodes, "nginx_executable_ambiguous") {
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
	identity, _ = (NginxRuntimeInspectorV2{Config: drift, Reader: versionReaderV2{mutate: true, observation: NginxVersionObservationV2{Version: "1.27.4"}}}).Inspect(context.Background())
	if !containsReasonV2(identity.ReasonCodes, "nginx_executable_identity_drift") {
		t.Fatalf("drift identity=%#v", identity)
	}
}

func TestRuntimeIdentityV2ConfigureArgumentBoundsAndExternalContour(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	tooMany := make([]string, MaxConfigureArgumentsV2+1)
	for index := range tooMany {
		tooMany[index] = "--argument-" + string(rune('a'+index%26)) + strings.Repeat("x", index/26)
	}
	identity := readyRuntimeIdentityV2(t, now, tooMany, nil)
	if identity.State != NginxIdentityUnknown || !containsReasonV2(identity.ReasonCodes, "nginx_configure_arguments_invalid") {
		t.Fatalf("argument bounds identity=%#v", identity)
	}
	managed := readyRuntimeIdentityV2(t, now, []string{"--with-stream"}, nil)
	managed.InstallationClass = NginxInstallationExternal
	managed.State = NginxExternalManaged
	managed.ReasonCodes = []string{"nginx_external_managed"}
	managed = finalizeRuntimeIdentity(managed)
	capability := ResolveNginxStrategyCapabilityV2(managed, StrategyL4OneToOne, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	if capability.Actionable || !capability.InspectionOnly {
		t.Fatalf("external capability=%#v", capability)
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
	if dynamicLoaded.Stream.State != NginxModuleDynamicLoaded || dynamicLoaded.State != NginxManagedEngineReady {
		t.Fatalf("loaded dynamic module state=%#v", dynamicLoaded)
	}
	dynamicNotLoaded := readyRuntimeIdentityV2(t, now, []string{"--with-stream=dynamic", "--with-stream_ssl_preread_module=dynamic"}, []string{})
	if dynamicNotLoaded.Stream.State != NginxModuleDynamicNotLoaded || dynamicNotLoaded.SSLPreread.State != NginxModuleDynamicNotLoaded ||
		dynamicNotLoaded.State != NginxIdentityUnknown {
		t.Fatalf("dynamic module state=%#v", dynamicNotLoaded)
	}
	dynamicUnknown := readyRuntimeIdentityV2(t, now, []string{"--with-stream=dynamic"}, nil)
	if dynamicUnknown.Stream.State != NginxModuleUnknown || dynamicUnknown.Stream.Effective != CapabilityUnknownV2 ||
		dynamicUnknown.State != NginxIdentityUnknown {
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
	config := NginxRuntimeInspectionConfigV2{CandidatePaths: []string{link}, AllowedExecutableRoots: []string{root},
		InstallationClass: NginxInstallationManaged, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	identity, err := (NginxRuntimeInspectorV2{Config: config}).Inspect(context.Background())
	if err != nil || !containsReasonV2(identity.ReasonCodes, "nginx_executable_path_escape") {
		t.Fatalf("symlink escape identity=%#v err=%v", identity, err)
	}
}

func TestStrategyCapabilityV2ValidationReloadAndVerificationAreIndependent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, field := range []string{"validation", "reload", "active", "process", "listener"} {
		identity := readyRuntimeIdentityV2(t, now, []string{"--with-stream"}, nil)
		switch field {
		case "validation":
			identity.ValidationMethod.Availability = CapabilityUnsupportedV2
		case "reload":
			identity.ReloadMethod.Availability = CapabilityUnsupportedV2
		case "active":
			identity.ActiveVerification.Availability = CapabilityUnsupportedV2
		case "process":
			identity.ProcessVerification.Availability = CapabilityUnsupportedV2
		case "listener":
			identity.ListenerVerification.Availability = CapabilityUnsupportedV2
		}
		identity = finalizeRuntimeIdentity(identity)
		capability := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
		if capability.Actionable {
			t.Fatalf("%s unavailable capability was actionable: %#v", field, capability)
		}
	}
}

func TestStrategyCapabilityV2SeparatesL4SNIProxyHTTPAndUDP(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	identity := readyRuntimeIdentityV2(t, now, []string{"--with-stream"}, nil)
	l4 := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyModeOff, CapabilityUnknownV2, now)
	sni := ResolveNginxStrategyCapabilityV2(identity, StrategySNIPreread, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	proxy := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyModeOn, CapabilityUnknownV2, now)
	proxyUnsupported := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyModeOn, CapabilityUnsupportedV2, now)
	proxyConfirmed := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyModeOn, CapabilitySupportedV2, now)
	http := ResolveNginxStrategyCapabilityV2(identity, StrategyHTTPTerminating, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	udp := ResolveNginxStrategyCapabilityV2(identity, StrategyUDPQUIC, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	unknownProxyMode := ResolveNginxStrategyCapabilityV2(identity, StrategyL4OneToOne, hostresources.ProxyMode("UNKNOWN"), CapabilitySupportedV2, now)
	if !l4.Actionable || sni.Actionable || sni.Support != StrategyUnsupportedV2 || !containsReasonV2(sni.ReasonCodes, "ssl_preread_unavailable") ||
		proxy.Actionable || proxy.Support != StrategyUnknownV2 || proxyUnsupported.Support != StrategyUnsupportedV2 ||
		http.Support != StrategyUnsupportedV2 || udp.Support != StrategyUnsupportedV2 || unknownProxyMode.Actionable ||
		!containsReasonV2(unknownProxyMode.ReasonCodes, "proxy_mode_unknown") || !proxyConfirmed.Actionable ||
		proxyConfirmed.SelectedProxyMode != hostresources.ProxyModeOn || proxyConfirmed.BackendProxyProtocolReceive != CapabilitySupportedV2 ||
		proxyConfirmed.CapabilityRevision == l4.CapabilityRevision {
		t.Fatalf("capabilities l4=%#v sni=%#v proxy=%#v http=%#v udp=%#v", l4, sni, proxy, http, udp)
	}
	receiveUnknown := identity
	receiveUnknown.ProxyProtocolReceive = NginxMethodCapabilityV2{Availability: CapabilityUnknownV2, Revision: v2Revision("nginx-proxy-receive-unknown")}
	receiveUnknown = finalizeRuntimeIdentity(receiveUnknown)
	emitPath := ResolveNginxStrategyCapabilityV2(receiveUnknown, StrategyL4OneToOne, hostresources.ProxyModeOn, CapabilitySupportedV2, now)
	if !emitPath.Actionable || emitPath.ProxyProtocolReceive.Availability != CapabilityUnknownV2 ||
		emitPath.ProxyProtocolEmit.Availability != CapabilitySupportedV2 || emitPath.BackendProxyProtocolReceive != CapabilitySupportedV2 {
		t.Fatalf("PROXY receive and emit proofs were coupled: %#v", emitPath)
	}
	prereadUnknown := readyRuntimeIdentityV2(t, now, []string{"--with-stream", "--with-stream_ssl_preread_module=dynamic"}, nil)
	unknownSNI := ResolveNginxStrategyCapabilityV2(prereadUnknown, StrategySNIPreread, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	if unknownSNI.Actionable || unknownSNI.Support != StrategyUnknownV2 || unknownSNI.SSLPreread.State != NginxModuleUnknown {
		t.Fatalf("unknown ssl_preread proof was not preserved: %#v", unknownSNI)
	}
}

func TestStrategyCapabilityV2RejectsInconsistentEffectiveSSLPreread(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	identity := readyRuntimeIdentityV2(t, now, []string{"--with-stream", "--with-stream_ssl_preread_module"}, nil)
	identity.SSLPreread.State = NginxModuleDynamicNotLoaded
	identity.SSLPreread.ReasonCode = "dynamic_module_not_loaded"
	identity.SSLPreread.Revision = v2Revision(struct {
		Name      string
		State     NginxModuleStateV2
		Effective CapabilityTruthV2
	}{"ssl_preread", identity.SSLPreread.State, identity.SSLPreread.Effective})
	identity.ModuleCapabilityRevision = v2Revision([]NginxModuleCapabilityV2{identity.Stream, identity.SSLPreread, identity.StreamSSL, identity.StreamRealIP})
	identity = finalizeRuntimeIdentity(identity)
	capability := ResolveNginxStrategyCapabilityV2(identity, StrategySNIPreread, hostresources.ProxyModeOff, CapabilitySupportedV2, now)
	if capability.Actionable || !containsReasonV2(capability.ReasonCodes, "runtime_identity_invalid") {
		t.Fatalf("inconsistent ssl_preread truth became actionable: identity=%#v capability=%#v", identity, capability)
	}
}

func TestSocketClaimV1ExplicitFamiliesWildcardAndStaleness(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, test := range []struct {
		bind     string
		family   hostresources.AddressFamily
		wildcard bool
	}{{"0.0.0.0", hostresources.AddressFamilyIPv4, true}, {"::", hostresources.AddressFamilyIPv6, true}, {"203.0.113.8", hostresources.AddressFamilyIPv4, false}} {
		claim, err := FinalizeFrontingSocketClaimV1(socketFixtureV1(now, test.bind, test.family, test.wildcard))
		if err != nil || claim.Validate(now) != nil {
			t.Fatalf("claim=%#v err=%v", claim, err)
		}
	}
	claim, _ := FinalizeFrontingSocketClaimV1(socketFixtureV1(now, "0.0.0.0", hostresources.AddressFamilyIPv4, true))
	if claim.Validate(now.Add(2*time.Minute)) == nil {
		t.Fatal("stale socket claim was accepted")
	}
	bad := socketFixtureV1(now, "0.0.0.0", hostresources.AddressFamilyIPv6, true)
	if _, err := FinalizeFrontingSocketClaimV1(bad); err == nil {
		t.Fatal("implicit dual-stack claim was accepted")
	}
	bad = socketFixtureV1(now, "127.0.0.1", hostresources.AddressFamilyIPv4, false)
	if _, err := FinalizeFrontingSocketClaimV1(bad); err == nil {
		t.Fatal("local public-socket claim was accepted")
	}
	bad = socketFixtureV1(now, "::ffff:203.0.113.8", hostresources.AddressFamilyIPv4, false)
	if _, err := FinalizeFrontingSocketClaimV1(bad); err == nil {
		t.Fatal("IPv4-mapped IPv6 claim was accepted as explicit IPv4")
	}
	for _, invalid := range []struct {
		bind   string
		family hostresources.AddressFamily
	}{{"224.0.0.1", hostresources.AddressFamilyIPv4}, {"fe80::1", hostresources.AddressFamilyIPv6}} {
		bad = socketFixtureV1(now, invalid.bind, invalid.family, false)
		if _, err := FinalizeFrontingSocketClaimV1(bad); err == nil {
			t.Fatalf("non-public socket claim %q was accepted", invalid.bind)
		}
	}
}

func TestSocketClaimV1ManagementCollisionBlocksOnlyTopologyPlanning(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := l4PlanInputV2(t, now, true)
	claim := socketFixtureV1(now, "0.0.0.0", hostresources.AddressFamilyIPv4, true)
	claim.TopologyMutationEligible = false
	claim.ReasonCodes = []string{"management_socket_collision"}
	input.Socket, _ = FinalizeFrontingSocketClaimV1(claim)
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || plan.Strategy.Selected != "" || !containsReasonV2(plan.Safety.ReasonCodes, "socket_topology_ineligible") {
		t.Fatalf("management collision plan=%#v err=%v", plan, err)
	}
}

func TestSelectorContractsV1GrammarIDsDefaultsAndBounds(t *testing.T) {
	target := strings.Repeat("a", 64)
	set, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "Panel.Example", ALPN: []string{"h2", "http/1.1"}, TargetReferenceRevision: target}}, SelectorDefaultV1{})
	if err != nil || len(set.Tuples) != 2 || set.Tuples[0].SNI != "panel.example" || set.Default.Policy != SelectorDefaultReject || strings.Contains(set.Tuples[0].SelectorID, "panel") {
		t.Fatalf("selector set=%#v err=%v", set, err)
	}
	again, _ := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "panel.example", ALPN: []string{"http/1.1", "h2"}, TargetReferenceRevision: target}}, SelectorDefaultV1{Policy: SelectorDefaultReject})
	if set.SelectorSetRevision != again.SelectorSetRevision {
		t.Fatalf("selector revision changed: %s %s", set.SelectorSetRevision, again.SelectorSetRevision)
	}
	for _, invalid := range []string{"éxample.test", "example.test.", "*.example.test", "-bad.test", "bad-.test", "bad..test"} {
		if _, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: invalid, TargetReferenceRevision: target}}, SelectorDefaultV1{}); err == nil {
			t.Fatalf("invalid SNI accepted: %q", invalid)
		}
	}
	for _, invalid := range []string{" h2", "h 2", "h2\n", "h2\"", "h2{", "h2$", "h2;", "h2\\", "h2,x", "proxy_pass"} {
		if _, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "a.test", ALPN: []string{invalid}, TargetReferenceRevision: target}}, SelectorDefaultV1{}); err == nil {
			t.Fatalf("invalid ALPN accepted: %q", invalid)
		}
	}
	if _, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "A.test", TargetReferenceRevision: target}, {SNI: "a.test", TargetReferenceRevision: target}}, SelectorDefaultV1{}); err == nil {
		t.Fatal("case duplicate was accepted")
	}
	if _, err := canonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "a.test", TargetReferenceRevision: target}, {SNI: "b.test", TargetReferenceRevision: target}}, SelectorDefaultV1{}, func(string, any) string { return "same" }); err == nil {
		t.Fatal("generated ID collision was accepted")
	}
	if _, err := CanonicalizeSelectorSetV1(nil, SelectorDefaultV1{Policy: SelectorDefaultFixedSafe}); err == nil {
		t.Fatal("fixed default without exact reference was accepted")
	}
	if nonTLS, err := CanonicalizeSelectorSetV1(nil, SelectorDefaultV1{Policy: SelectorDefaultNonTLSFixedTarget, TargetReferenceRevision: target}); err != nil || nonTLS.Default.Policy != SelectorDefaultNonTLSFixedTarget {
		t.Fatalf("non-TLS fixed policy contract=%#v err=%v", nonTLS, err)
	}
	overRoutes := make([]SelectorRouteInputV1, 0, MaxFrontingRoutesV1+1)
	for index := 0; index <= MaxFrontingRoutesV1; index++ {
		overRoutes = append(overRoutes, SelectorRouteInputV1{SNI: "bound" + decimalV2(index) + ".example", TargetReferenceRevision: target})
	}
	if _, err := CanonicalizeSelectorSetV1(overRoutes, SelectorDefaultV1{}); err == nil || err.Error() != "selector_route_limit_exceeded" {
		t.Fatalf("distinct SNI route limit was not enforced: %v", err)
	}
}

func TestPlannerV2DeterministicReadOnlyEligibilityAndExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := l4PlanInputV2(t, now, true)
	first, err := PlanFrontingStrategyV2(input)
	if err != nil || first.Strategy.Selected != StrategyL4OneToOne || first.Strategy.Actual != FrontingActualNotAppliedV2 || first.Validate() != nil {
		t.Fatalf("plan=%#v err=%v", first, err)
	}
	second, _ := PlanFrontingStrategyV2(input)
	if first.CanonicalPlanDigest != second.CanonicalPlanDigest || first.ExpiresAt != input.Inventory[0].ExpiresAt {
		t.Fatalf("plan not deterministic/earliest expiry: %#v %#v", first, second)
	}
	endpoint, err := hostresources.ResolveFrontingBackendEndpointV1(input.BackendReferences[0], input.Inventory[0], now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{endpoint.Address.String(), "proxy_pass", "stream.conf", "candidate", "nginx -V"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("plan leaked %q: %s", forbidden, encoded)
		}
	}
	if jsonContainsNumberV2(decoded, float64(endpoint.Port)) {
		t.Fatalf("plan leaked backend port %d: %s", endpoint.Port, encoded)
	}
	input.Runtime = readyRuntimeIdentityV2(t, now, []string{"--with-stream"}, nil)
	input.DesiredStrategy = StrategySNIPreread
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "a.test", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision}}, SelectorDefaultV1{})
	blocked, _ := PlanFrontingStrategyV2(input)
	if blocked.Strategy.Selected != "" || !containsReasonV2(blocked.Safety.ReasonCodes, "ssl_preread_unavailable") {
		t.Fatalf("SNI without preread was selected: %#v", blocked)
	}
	input.Runtime = readyRuntimeIdentityV2(t, now, []string{"--with-stream", "--with-stream_ssl_preread_module"}, nil)
	input.Socket.ManagementExclusionRevision = input.Runtime.ManagementExclusionsRevision
	input.Socket, _ = FinalizeFrontingSocketClaimV1(input.Socket)
	admitted, _ := PlanFrontingStrategyV2(input)
	if admitted.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("SNI plan not admitted: %#v", admitted)
	}
}

func TestPlannerV2DigestBindsExpiryToExactTargetRevision(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := l4PlanInputV2(t, now, true)
	secondFact := backendFactV2(t, now, 1)
	secondReference, err := hostresources.ReferenceFrontingBackendV1(secondFact, hostresources.ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	input.DesiredStrategy = StrategySNIPreread
	input.Inventory = append(input.Inventory, secondFact)
	input.BackendReferences = append(input.BackendReferences, secondReference)
	input.Inventory[0].ExpiresAt = now.Add(30 * time.Second).Unix()
	input.Inventory[1].ExpiresAt = now.Add(45 * time.Second).Unix()
	input.Selectors, err = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "a.test", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision},
		{SNI: "b.test", TargetReferenceRevision: input.BackendReferences[1].CanonicalReferenceRevision},
	}, SelectorDefaultV1{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := PlanFrontingStrategyV2(input)
	if err != nil || first.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("first plan failed: %#v err=%v", first, err)
	}
	input.Inventory[0].ExpiresAt, input.Inventory[1].ExpiresAt = input.Inventory[1].ExpiresAt, input.Inventory[0].ExpiresAt
	second, err := PlanFrontingStrategyV2(input)
	if err != nil || second.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("second plan failed: %#v err=%v", second, err)
	}
	if first.CanonicalPlanDigest == second.CanonicalPlanDigest || first.ExpiresAt != second.ExpiresAt {
		t.Fatalf("target-bound expiry did not affect only the digest: first=%#v second=%#v", first.Safety.InputExpiries, second.Safety.InputExpiries)
	}
}

func TestPlannerV2ExactTargetAndFailClosedStrategies(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := l4PlanInputV2(t, now, true)
	target := input.BackendReferences[0].CanonicalReferenceRevision
	input.Selectors, _ = CanonicalizeSelectorSetV1(nil, SelectorDefaultV1{Policy: SelectorDefaultFixedSafe, TargetReferenceRevision: target})
	extraDefault, _ := PlanFrontingStrategyV2(input)
	if extraDefault.Strategy.Selected != "" || !containsReasonV2(extraDefault.Safety.ReasonCodes, "l4_additional_target_forbidden") {
		t.Fatalf("L4 plan accepted an additional fixed default: %#v", extraDefault)
	}
	input = l4PlanInputV2(t, now, true)
	input.Inventory = nil
	missing, _ := PlanFrontingStrategyV2(input)
	if missing.Strategy.Selected != "" || !containsReasonV2(missing.Safety.ReasonCodes, "backend_reference_missing") {
		t.Fatalf("missing exact target was substituted: %#v", missing)
	}
	input = l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategyHTTPTerminating
	unsupported, _ := PlanFrontingStrategyV2(input)
	if unsupported.Strategy.Selected != "" || !containsReasonV2(unsupported.Safety.ReasonCodes, "http_terminating_not_shipped") {
		t.Fatalf("HTTP terminating plan=%#v", unsupported)
	}
	input.DesiredStrategy = StrategyUDPQUIC
	udp, _ := PlanFrontingStrategyV2(input)
	if udp.Strategy.Selected != "" || !containsReasonV2(udp.Safety.ReasonCodes, "udp_quic_out_of_scope") {
		t.Fatalf("UDP plan=%#v", udp)
	}
	input = l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategySNIPreread
	target = input.BackendReferences[0].CanonicalReferenceRevision
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "a.test", TargetReferenceRevision: target}},
		SelectorDefaultV1{Policy: SelectorDefaultNonTLSFixedTarget, TargetReferenceRevision: target})
	nonTLS, _ := PlanFrontingStrategyV2(input)
	if nonTLS.Strategy.Selected != "" || !containsReasonV2(nonTLS.Safety.ReasonCodes, "non_tls_discriminator_unproven") {
		t.Fatalf("unproven non-TLS discrimination was selected: %#v", nonTLS)
	}
}

func TestPlannerV2PerformanceInvariants(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := largePlanInputV2(t, now)
	for range 40 {
		plan, err := PlanFrontingStrategyV2(input)
		if err != nil || plan.Strategy.Selected != StrategySNIPreread {
			t.Fatalf("large plan failed: selected=%s err=%v reasons=%v", plan.Strategy.Selected, err, plan.Safety.ReasonCodes)
		}
	}
	allocations := testing.AllocsPerRun(20, func() {
		if _, err := PlanFrontingStrategyV2(input); err != nil {
			panic(err)
		}
	})
	t.Logf("resources=4096 selectors=%d supplied_targets=%d selected_targets=%d allocations_per_plan=%.0f",
		len(input.Selectors.Tuples), len(input.BackendReferences), len(input.Selectors.TargetRevisions), allocations)
}

func l4PlanInputV2(t *testing.T, now time.Time, sslPreread bool) FrontingPlanInputV2 {
	t.Helper()
	arguments := []string{"--with-stream"}
	if sslPreread {
		arguments = append(arguments, "--with-stream_ssl_preread_module")
	}
	identity := readyRuntimeIdentityV2(t, now, arguments, nil)
	fact := backendFactV2(t, now, 0)
	reference, err := hostresources.ReferenceFrontingBackendV1(fact, hostresources.ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := FinalizeFrontingSocketClaimV1(socketFixtureV1(now, "0.0.0.0", hostresources.AddressFamilyIPv4, true))
	if err != nil {
		t.Fatal(err)
	}
	emptySelectors, _ := CanonicalizeSelectorSetV1(nil, SelectorDefaultV1{})
	return FrontingPlanInputV2{Now: now, DesiredStrategy: StrategyL4OneToOne, Runtime: identity, Socket: claim,
		Inventory: []hostresources.FrontingBackendFactV1{fact}, BackendReferences: []hostresources.FrontingBackendReferenceV1{reference},
		Selectors: emptySelectors, ProxyMode: hostresources.ProxyModeOff}
}

func largePlanInputV2(t *testing.T, now time.Time) FrontingPlanInputV2 {
	t.Helper()
	input := l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategySNIPreread
	input.Inventory = make([]hostresources.FrontingBackendFactV1, hostresources.MaxResourceFacts)
	input.BackendReferences = make([]hostresources.FrontingBackendReferenceV1, MaxFixedTargetsV1)
	for index := range input.Inventory {
		input.Inventory[index] = backendFactV2(t, now, index)
		if index < MaxFixedTargetsV1 {
			input.BackendReferences[index], _ = hostresources.ReferenceFrontingBackendV1(input.Inventory[index], hostresources.ProxyModeOff, now)
		}
	}
	routes := make([]SelectorRouteInputV1, 0, MaxFrontingRoutesV1)
	for route := 0; route < MaxFrontingRoutesV1; route++ {
		sni := "route" + decimalV2(route) + ".example"
		routes = append(routes, SelectorRouteInputV1{SNI: sni, TargetReferenceRevision: input.BackendReferences[route].CanonicalReferenceRevision})
	}
	input.Selectors, _ = CanonicalizeSelectorSetV1(routes, SelectorDefaultV1{})
	return input
}

func backendFactV2(t *testing.T, now time.Time, index int) hostresources.FrontingBackendFactV1 {
	t.Helper()
	resourceID, endpointID := "resource-"+decimalV2(index), "endpoint-"+decimalV2(index)
	endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: endpointID,
		Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4,
			BindAddress: "127.0.0.1", Port: uint16(10000 + index%50000)},
		Intent: hostresources.EndpointIntentLocal, Protocol: "tcp", ProxyProtocol: hostresources.CapabilityNo,
		ResourceID: resourceID, Owner: "core", OwnerRevision: "owner-v1", ConfigurationRevision: v2Revision(struct{ E int }{index}),
		ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10_000}
	resource := hostresources.ProtectableResource{ID: resourceID, Kind: string(hostresources.FrontingBackendInboundResource), Owner: endpoint.Owner,
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: endpoint.OwnerRevision},
		Endpoints:    []hostresources.PublicEndpoint{endpoint}}
	value, err := hostresources.NewFrontingBackendFactV1(hostresources.FrontingBackendFactV1{
		ProviderID: "provider", ContributorID: "contributor", ProviderRevision: "provider-v1",
		HealthRevision: v2Revision(struct{ H int }{index}), CapacityRevision: v2Revision(struct{ C int }{index}),
		Ownership: hostresources.FrontingBackendProviderManaged, CanReachManagement: hostresources.CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}, resource, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func socketFixtureV1(now time.Time, bind string, family hostresources.AddressFamily, wildcard bool) FrontingSocketClaimV1 {
	return FrontingSocketClaimV1{ResourceID: "public:fronting", EndpointID: "fronting-endpoint", AddressFamily: family,
		CanonicalBind: bind, Wildcard: wildcard, Protocol: hostresources.NetworkTCP, PublicPort: 443,
		CurrentConfigurationRevision: v2Revision("config"), TopologyOwnershipEligibilityRevision: v2Revision("topology"),
		ListenerSocketFactRevision: v2Revision("listener"), ManagementExclusionRevision: v2Revision("management"),
		TopologyMutationEligible: true, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
}

func containsReasonV2(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func jsonContainsNumberV2(value any, wanted float64) bool {
	switch typed := value.(type) {
	case float64:
		return typed == wanted
	case []any:
		for _, item := range typed {
			if jsonContainsNumberV2(item, wanted) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if jsonContainsNumberV2(item, wanted) {
				return true
			}
		}
	}
	return false
}

func decimalV2(value int) string {
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}

func FuzzSelectorContractsV1(f *testing.F) {
	for _, seed := range []struct{ sni, alpn string }{
		{"example.test", "h2"}, {"EXAMPLE.TEST", "http/1.1"}, {"*.example.test", "h2"}, {"éxample.test", "h2"},
		{"example.test.", "proxy_pass"}, {"a..test", "h2\n"},
	} {
		f.Add(seed.sni, seed.alpn)
	}
	target := strings.Repeat("a", 64)
	f.Fuzz(func(t *testing.T, sni, alpn string) {
		set, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: sni, ALPN: []string{alpn}, TargetReferenceRevision: target}}, SelectorDefaultV1{})
		if err == nil {
			if err := set.Validate(); err != nil {
				t.Fatalf("canonical selector failed validation: %#v err=%v", set, err)
			}
			encoded, _ := json.Marshal(set)
			if strings.Contains(string(encoded), "proxy_pass") || strings.Contains(string(encoded), "include ") {
				t.Fatalf("directive-shaped selector survived: %s", encoded)
			}
		}
	})
}

func FuzzRuntimeVersionAndArgumentsV2(f *testing.F) {
	for _, seed := range []struct{ version, argument string }{
		{"1.27.4", "--with-stream"}, {"nginx/1.26.0", "--with-stream=dynamic"}, {"bad version", "--with-stream"}, {"1.2.3", "bad\nargument"},
	} {
		f.Add(seed.version, seed.argument)
	}
	f.Fuzz(func(t *testing.T, version, argument string) {
		normalizedVersion, versionOK := normalizeNginxVersionV2(version)
		arguments, argumentsOK := normalizeConfigureArgumentsV2([]string{argument})
		if versionOK {
			if again, ok := normalizeNginxVersionV2(normalizedVersion); !ok || again != normalizedVersion {
				t.Fatalf("version normalization is not idempotent: %q", normalizedVersion)
			}
		}
		if argumentsOK {
			if again, ok := normalizeConfigureArgumentsV2(arguments); !ok || v2Revision(again) != v2Revision(arguments) {
				t.Fatalf("argument normalization is not idempotent: %#v", arguments)
			}
		}
	})
}
