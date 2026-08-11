package firewall

import (
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestPreviewIsDeterministicAndNonEnforcing(t *testing.T) {
	resources := []hostresources.ProtectableResource{
		{ID: "core:subscription:default", Protocol: "http", Listen: "127.0.0.1", Port: 2096},
		{ID: "core:inbound:1", Protocol: "udp", Listen: "0.0.0.0", Port: 443},
		{ID: "core:panel:web", Protocol: "http", Listen: "127.0.0.1", Port: 2095},
	}
	ports := []protectionrepository.PortAllowlistModel{{ID: 1, Protocol: "tcp", Listen: "*", PortStart: 2222, PortEnd: 2222, Reason: "managed SSH"}}
	graylist := []protectionrepository.GraylistModel{{IPCIDR: "203.0.113.8/32"}, {IPCIDR: "2001:db8::/64"}}
	left := BuildPlan(resources, ports, graylist)
	right := BuildPlan(resources, ports, graylist)
	if left.Revision == "" || left.Revision != right.Revision {
		t.Fatalf("plan revisions = %q / %q", left.Revision, right.Revision)
	}
	preview := Preview(left, PreviewOptions{OperatingSystem: "linux", IncludeGeneratedNFT: true})
	if preview.Backend != "preview_only" || len(preview.WouldBlock) != 0 || len(preview.ProtectedKeep) != 3 {
		t.Fatalf("preview = %#v", preview)
	}
	lower := strings.ToLower(preview.GeneratedNFT)
	for _, forbidden := range []string{"drop", "reject", "dnat", "redirect", "notrack"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("generated preview contains enforcing token %q:\n%s", forbidden, preview.GeneratedNFT)
		}
	}
	for _, required := range []string{"table inet solovey_protection", "policy accept", "counter accept", "solovey_allow_tcp_ports", "solovey_allow_udp_ports"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("generated preview is missing %q:\n%s", required, preview.GeneratedNFT)
		}
	}
	if strings.Contains(lower, "flush ruleset") || strings.Contains(lower, "table inet filter") {
		t.Fatalf("preview attempts unmanaged rewrite:\n%s", preview.GeneratedNFT)
	}
}

func TestNonLinuxPreviewReturnsInventoryWithoutScript(t *testing.T) {
	plan := BuildPlan([]hostresources.ProtectableResource{{ID: "core:panel:web", Protocol: "http", Listen: "127.0.0.1", Port: 2095}}, nil, nil)
	preview := Preview(plan, PreviewOptions{OperatingSystem: "windows", IncludeGeneratedNFT: true})
	if preview.Backend != "unsupported" || preview.GeneratedNFT != "" || len(preview.ProtectedKeep) != 1 || len(preview.WouldBlock) != 0 {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPlanKeepsCommonSSHAsWarningOnlyFallback(t *testing.T) {
	plan := BuildPlan(nil, nil, nil)
	if !containsPort(plan.AllowTCPPorts, 22) {
		t.Fatalf("TCP keep ports = %#v", plan.AllowTCPPorts)
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "SSH") {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
}

func TestPlanAcceptsExplicitNonstandardSSHKeep(t *testing.T) {
	resources := []hostresources.ProtectableResource{{ID: "core:panel:web", Kind: "panel_web", Protocol: "http", Listen: "0.0.0.0", Port: 443}}
	plan := BuildPlan(resources, []protectionrepository.PortAllowlistModel{{Protocol: "tcp", Listen: "*", PortStart: 2222, PortEnd: 2222, Reason: "managed SSH"}}, nil)
	if !containsPort(plan.AllowTCPPorts, 2222) || containsPort(plan.AllowTCPPorts, 22) {
		t.Fatalf("explicit SSH keep ports = %#v", plan.AllowTCPPorts)
	}
	if err := Preflight(plan); err != nil {
		t.Fatalf("explicit nonstandard SSH was blocked: %v", err)
	}
}

func TestPlanPreservesACMEPortsAndExternalOwnershipWarningsFromInventory(t *testing.T) {
	resources := []hostresources.ProtectableResource{
		{ID: "core:acme-http", Protocol: "http", Listen: "0.0.0.0", Port: 80, Capabilities: hostresources.ProtectableResourceCapabilities{RequiresACMEHTTP01: hostresources.CapabilityYes}},
		{ID: "core:acme-tls", Protocol: "stream", Listen: "0.0.0.0", Port: 443, Capabilities: hostresources.ProtectableResourceCapabilities{RequiresTLSALPN01: hostresources.CapabilityYes}},
		{ID: "host:ownership", Protocol: "stream", Listen: "127.0.0.1", Port: 1, Warnings: []string{"Docker/firewalld/ufw ownership is unknown; review externally managed rules"}},
	}
	plan := BuildPlan(resources, nil, nil)
	for _, port := range []int{80, 443} {
		if !containsPort(plan.AllowTCPPorts, port) {
			t.Fatalf("ACME listener port %d was not preserved: %#v", port, plan.AllowTCPPorts)
		}
	}
	if len(plan.Warnings) == 0 || !strings.Contains(strings.Join(plan.Warnings, " "), "Docker/firewalld/ufw ownership is unknown") {
		t.Fatalf("external ownership warning was not preserved: %#v", plan.Warnings)
	}
}
