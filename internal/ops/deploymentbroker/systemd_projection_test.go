package deploymentbroker

import (
	"slices"
	"strings"
	"testing"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func TestSystemdActualStateProjectionRequiresExactHardenedAuthority(t *testing.T) {
	properties := hardenedSystemdProperties()
	paths := hardenedWritePaths()
	if reasons := systemdProfileReasons(domain.NativeHardened, properties, paths); len(reasons) != 0 {
		t.Fatalf("packaged hardened facts rejected: %v", reasons)
	}
	for key, value := range map[string]string{
		"User": "root", "CapabilityBoundingSet": "CAP_SYS_ADMIN", "ReadWritePaths": "/", "PrivateDevices": "no", "TasksMax": "infinity",
	} {
		drift := hardenedSystemdProperties()
		drift[key] = value
		if reasons := systemdProfileReasons(domain.NativeHardened, drift, paths); len(reasons) == 0 {
			t.Fatalf("%s drift was accepted", key)
		}
	}
}

func TestSystemdObservationRequestsEveryProjectedProperty(t *testing.T) {
	requested := map[string]struct{}{}
	for _, property := range strings.Split(systemdObservationProperties, ",") {
		if property == "" {
			t.Fatal("empty systemd observation property")
		}
		if _, exists := requested[property]; exists {
			t.Fatalf("duplicate systemd observation property %q", property)
		}
		requested[property] = struct{}{}
	}
	for property := range hardenedSystemdProperties() {
		if _, exists := requested[property]; !exists {
			t.Fatalf("projected property %q is absent from the systemd observation request", property)
		}
	}
	for _, property := range []string{"RuntimeDirectory", "StateDirectory", "CacheDirectory", "LogsDirectory"} {
		if _, exists := requested[property]; !exists {
			t.Fatalf("deployment directory property %q is absent from the systemd observation request", property)
		}
	}
}

func TestSystemdActualStateProjectionHandlesOrderingAndLegacy(t *testing.T) {
	properties := hardenedSystemdProperties()
	properties["RestrictAddressFamilies"] = "AF_UNIX AF_INET6 AF_INET"
	properties["ReadWritePaths"] = "/usr/local/solovey-ui/cert /var/lib/solovey-ui /usr/local/solovey-ui/.runtime/server-protection"
	if reasons := systemdProfileReasons(domain.NativeHardened, properties, hardenedWritePaths()); len(reasons) != 0 {
		t.Fatalf("set ordering changed semantic result: %v", reasons)
	}
	legacy := map[string]string{"LoadState": "loaded", "ActiveState": "active", "NeedDaemonReload": "no", "User": "root", "Group": "root",
		"LimitNOFILE": "1048576", "TasksMax": "4096", "MemoryHigh": "805306368", "MemoryMax": "1073741824", "CPUQuotaPerSecUSec": "2s"}
	if reasons := systemdProfileReasons(domain.NativeLegacyRoot, legacy, nil); len(reasons) != 0 {
		t.Fatalf("legacy compatibility facts rejected: %v", reasons)
	}
	legacy["User"] = "solovey-ui"
	if reasons := systemdProfileReasons(domain.NativeLegacyRoot, legacy, nil); !slices.Contains(reasons, "panel_identity_mismatch") {
		t.Fatalf("legacy identity drift accepted: %v", reasons)
	}
}

func TestSystemdVersionProjectionIsClosedAndNumeric(t *testing.T) {
	for input, expected := range map[string]string{
		"systemd 249 (249.11)\nPAM": "systemd-249",
		"systemd 257":               "systemd-257",
		"systemd future":            "systemd-unknown",
		"not-systemd 257":           "systemd-unknown",
	} {
		if actual := parseSystemdVersion(input); actual != expected {
			t.Fatalf("parseSystemdVersion(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestSystemdWritePathsComeFromAuthenticatedPackagedUnit(t *testing.T) {
	unit := []byte("[Unit]\nDescription=test\n[Service]\nReadWritePaths=/var/lib/solovey-ui /usr/local/solovey-ui/.runtime/server-protection /usr/local/solovey-ui/cert\n")
	paths, ok := unitPathDirective(unit, "ReadWritePaths")
	if !ok || !samePaths(strings.Join(paths, " "), hardenedWritePaths()) {
		t.Fatalf("packaged write paths=%v ok=%v", paths, ok)
	}
	for _, invalid := range [][]byte{
		[]byte("[Service]\nReadWritePaths=/safe\nReadWritePaths=/other\n"),
		[]byte("[Service]\nReadWritePaths=relative\n"),
		[]byte("[Service]\nReadWritePaths=/safe/%i\n"),
	} {
		if _, ok := unitPathDirective(invalid, "ReadWritePaths"); ok {
			t.Fatalf("unsafe packaged directive accepted: %q", invalid)
		}
	}
}

func TestSystemdActualStateProjectionIsBoundedAndFailClosed(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	input := systemdActualInput{Profile: domain.NativeHardened, Properties: hardenedSystemdProperties(), VersionOutput: "systemd 257 (257.5)\nPAM",
		ManagerBootRevision: domain.Revision("boot"), DirectiveSupport: domain.Available,
		DirectiveCapabilityRevision: domain.Revision("directives"), FragmentTrusted: true,
		FragmentRevision: domain.Revision("fragment"), ExecutableRevision: domain.Revision("executable"), BrokerSocketRevision: domain.Revision("sockets"), ObservedAt: now}
	facts, reasons := projectSystemdActualState(input)
	if len(reasons) != 0 || facts.Validate(now) != nil || facts.Version != "systemd-257" || facts.SubState != "running" || facts.DirectiveSupport != domain.Available {
		t.Fatalf("facts=%#v reasons=%v validate=%v", facts, reasons, facts.Validate(now))
	}
	mutations := map[string]func(*systemdActualInput){
		"unknown directives": func(v *systemdActualInput) { v.DirectiveSupport = domain.Unknown },
		"foreign fragment":   func(v *systemdActualInput) { v.FragmentTrusted = false },
		"foreign dropin": func(v *systemdActualInput) {
			v.Properties["DropInPaths"] = "/etc/systemd/system/solovey-ui.service.d/foreign.conf"
		},
		"unsafe executable": func(v *systemdActualInput) { v.ExecutableRevision = "" },
		"inactive unit":     func(v *systemdActualInput) { v.Properties["SubState"] = "dead" },
		"reload required":   func(v *systemdActualInput) { v.Properties["NeedDaemonReload"] = "yes" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copy := input
			copy.Properties = hardenedSystemdProperties()
			mutate(&copy)
			_, reasons := projectSystemdActualState(copy)
			if len(reasons) == 0 {
				t.Fatal("unsafe systemd state was accepted")
			}
		})
	}
}

func TestNativeCapabilitiesRequireObservedVerifiedPostureAndTargetDirectives(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	if capabilities := nativeCapabilities(nil, now); capabilities.Observe != domain.Unavailable || capabilities.Doctor != domain.Available || capabilities.Migrate != domain.Unavailable {
		t.Fatalf("missing observation capabilities=%#v", capabilities)
	}
	properties := hardenedSystemdProperties()
	properties["User"], properties["Group"], properties["NoNewPrivileges"] = "root", "root", "no"
	input := systemdActualInput{Profile: domain.NativeLegacyRoot, Properties: properties, VersionOutput: "systemd 257",
		ManagerBootRevision: domain.Revision("boot"), DirectiveSupport: domain.Available, DirectiveCapabilityRevision: domain.Revision("directives"),
		FragmentTrusted: true, FragmentRevision: domain.Revision("fragment"), ExecutableRevision: domain.Revision("executable"),
		BrokerSocketRevision: domain.Revision("sockets"), ObservedAt: now}
	facts, reasons := projectSystemdActualState(input)
	if len(reasons) != 0 {
		t.Fatal(reasons)
	}
	posture := domain.Posture{Schema: domain.SchemaV1, Profile: domain.NativeLegacyRoot, InstalledProfile: domain.NativeLegacyRoot,
		ActiveProfile: domain.NativeLegacyRoot, VerifiedProfile: domain.NativeLegacyRoot, Runtime: domain.RuntimeNative, PanelRoot: true,
		BrokerAvailable: true, BrokerRevision: domain.Revision("broker"), ServiceRevision: domain.Revision("service"), DataRevision: domain.Revision("data"),
		HardeningRevision: domain.Revision("hardening"), Systemd: &facts, ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix()}
	domain.SetPostureRevision(&posture)
	if capabilities := nativeCapabilities(&posture, now); capabilities.Observe != domain.Available || capabilities.Migrate != domain.Available {
		t.Fatalf("verified posture capabilities=%#v", capabilities)
	}
	posture.PanelUID = 1001
	domain.SetPostureRevision(&posture)
	if capabilities := nativeCapabilities(&posture, now); capabilities.Migrate != domain.Unavailable {
		t.Fatalf("inconsistent process identity capabilities=%#v", capabilities)
	}
	posture.PanelUID = 0
	posture.Reasons = []string{"deployment_marker_mismatch"}
	domain.SetPostureRevision(&posture)
	if capabilities := nativeCapabilities(&posture, now); capabilities.Migrate != domain.Unavailable {
		t.Fatalf("drifted posture capabilities=%#v", capabilities)
	}
	posture.Reasons = nil
	posture.Systemd.DirectiveSupport = domain.Unknown
	posture.Systemd.Revision = ""
	posture.Systemd.Revision = domain.Revision(*posture.Systemd)
	domain.SetPostureRevision(&posture)
	if capabilities := nativeCapabilities(&posture, now); capabilities.Migrate != domain.Unavailable {
		t.Fatalf("unknown directive capabilities=%#v", capabilities)
	}
}

func hardenedSystemdProperties() map[string]string {
	return map[string]string{
		"LoadState": "loaded", "ActiveState": "active", "SubState": "running", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/solovey-ui.service",
		"DropInPaths": "", "UnitFileState": "enabled", "User": "solovey-ui", "Group": "solovey-ui", "NoNewPrivileges": "yes",
		"PrivateTmp": "yes", "PrivateDevices": "yes", "ProtectSystem": "strict", "ProtectHome": "yes", "ProtectKernelTunables": "yes",
		"ProtectKernelModules": "yes", "ProtectKernelLogs": "yes", "ProtectControlGroups": "yes", "ProtectClock": "yes", "ProtectHostname": "yes",
		"RestrictNamespaces": "yes", "RestrictRealtime": "yes", "RestrictSUIDSGID": "yes", "LockPersonality": "yes", "MemoryDenyWriteExecute": "yes",
		"SystemCallArchitectures": "native", "CapabilityBoundingSet": "", "AmbientCapabilities": "", "UMask": "0077",
		"RestrictAddressFamilies": "AF_INET AF_INET6 AF_UNIX", "ReadWritePaths": "/var/lib/solovey-ui /usr/local/solovey-ui/.runtime/server-protection /usr/local/solovey-ui/cert",
		"ReadOnlyPaths": "/etc/solovey-ui", "LimitNOFILE": "1048576", "TasksMax": "4096", "MemoryHigh": "805306368", "MemoryMax": "1073741824",
		"CPUQuotaPerSecUSec": "2s", "Restart": "on-failure", "RestartUSec": "5s", "WatchdogUSec": "0", "TimeoutStartUSec": "45s", "TimeoutStopUSec": "30s",
	}
}

func hardenedWritePaths() []string {
	return []string{"/var/lib/solovey-ui", "/usr/local/solovey-ui/.runtime/server-protection", "/usr/local/solovey-ui/cert"}
}

func BenchmarkSystemdActualStateProjection(b *testing.B) {
	properties := hardenedSystemdProperties()
	now := time.Unix(1_900_000_000, 0).UTC()
	input := systemdActualInput{Profile: domain.NativeHardened, Properties: properties, VersionOutput: "systemd 257",
		ManagerBootRevision: domain.Revision("boot"), DirectiveSupport: domain.Available, DirectiveCapabilityRevision: domain.Revision("directives"),
		FragmentTrusted: true, FragmentRevision: domain.Revision("fragment"), ExecutableRevision: domain.Revision("executable"), BrokerSocketRevision: domain.Revision("sockets"), ObservedAt: now}
	b.ReportAllocs()
	for b.Loop() {
		if reasons := systemdProfileReasons(domain.NativeHardened, properties, hardenedWritePaths()); len(reasons) != 0 {
			b.Fatal(reasons)
		}
		facts, reasons := projectSystemdActualState(input)
		if len(reasons) != 0 || facts.Revision == "" {
			b.Fatal(reasons)
		}
	}
}
