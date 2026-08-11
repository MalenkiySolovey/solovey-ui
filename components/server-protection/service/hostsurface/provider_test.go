package hostsurface

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestNormalizeClassifiesEveryObservedListenerOrUnknown(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	managed := hostresources.ProtectableResource{ID: "core:inbound:1", Owner: "core", Protocol: "stream", Listen: "0.0.0.0", Port: 443, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: "cfg-1"}}
	managed.Endpoints = []hostresources.PublicEndpoint{hostresources.BuildEndpointFact(managed, hostresources.NetworkTCP, now)}
	pid, uid := 10, 1000
	raw := PlatformSnapshot{Sockets: []RawSocket{
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 443, Inode: "1", Processes: []RawProcess{{PID: pid, UID: uid, StartTime: "10", ExecutableToken: "managed"}}},
		{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv6, Bind: "::1", Port: 5353, Inode: "2", Processes: []RawProcess{{PID: pid, UID: uid, StartTime: "10", ExecutableToken: "local"}}},
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 9000, Inode: "3", Processes: []RawProcess{{PID: pid, UID: uid, StartTime: "10", ExecutableToken: "unexpected"}}},
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "::", Port: 22, Inode: "4"},
	}}
	result := Normalize(raw, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{managed}}, now)
	if len(result.Facts) != 4 {
		t.Fatalf("facts = %d", len(result.Facts))
	}
	want := map[uint16]hostfacts.Classification{443: hostfacts.ClassificationUnknownOwner, 5353: hostfacts.ClassificationLocalOnly, 9000: hostfacts.ClassificationUnexpectedPublic, 22: hostfacts.ClassificationUnknownOwner}
	for _, fact := range result.Facts {
		if fact.Classification != want[fact.Port] {
			t.Fatalf("port %d classification = %s", fact.Port, fact.Classification)
		}
		if fact.ID == "" || fact.Source == "" {
			t.Fatalf("incomplete fact: %#v", fact)
		}
		if fact.Port == 443 && (fact.RegisteredResourceID != managed.ID || !containsReason(fact.ReasonCodes, "process_owner_not_verified")) {
			t.Fatalf("socket claim was mistaken for verified process ownership: %#v", fact)
		}
	}
}

func TestNormalizeMatchesEachConfiguredNetworkIntentIndependently(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	revision := strings.Repeat("c", 64)
	resource := hostresources.ProtectableResource{ID: "core:inbound:dual", Owner: "core", Protocol: "stream", Listen: "127.0.0.1", Port: 2443, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: revision}}
	tcp := hostresources.BuildConfiguredListenIntent(resource)
	udp := tcp
	udp.Network = hostresources.NetworkUDP
	resource.ListenIntents = []hostresources.ConfiguredListenIntentV1{tcp, udp}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 2443},
		{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 2443},
	}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, now)
	if len(result.Facts) != 2 || result.Facts[0].RegisteredResourceID != resource.ID || result.Facts[1].RegisteredResourceID != resource.ID || result.Facts[0].Network == result.Facts[1].Network {
		t.Fatalf("TCP/UDP intents were conflated: %#v", result.Facts)
	}
}

func TestNormalizeDoesNotMatchUnknownRegisteredEndpoint(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	resource := hostresources.ProtectableResource{ID: "core:inbound:unknown", Owner: "core", Protocol: "stream", Listen: "0.0.0.0", Port: 443, Capabilities: hostresources.ProtectableResourceCapabilities{Known: false}}
	resource.Endpoints = []hostresources.PublicEndpoint{hostresources.BuildEndpointFact(resource, hostresources.NetworkTCP, now, "inventory_truncated")}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 443, Processes: []RawProcess{{PID: 7, UID: 1000, StartTime: "1", ExecutableToken: "unknown"}}}}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, now)
	fact := result.Facts[0]
	if fact.RegisteredResourceID != "" || fact.Classification != hostfacts.ClassificationUnexpectedPublic {
		t.Fatalf("unknown registered endpoint was accepted as current: %#v", fact)
	}
}

func TestSafeServiceTokenRejectsPathsAndArguments(t *testing.T) {
	for _, value := range []string{"/system.slice/sshd.service", `C:\\secret\\agent.exe`, "sshd.service --flag"} {
		if safeServiceToken(value) != "" {
			t.Fatalf("unsafe service token %q was exposed", value)
		}
	}
	if got := safeServiceToken("sshd.service"); got != "sshd.service" {
		t.Fatalf("safe unit rejected: %q", got)
	}
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestNormalizePreservesPIDOwnerAmbiguity(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 8443, Inode: "8", Processes: []RawProcess{{PID: 1}, {PID: 2}}}}}, hostresources.ResourceSnapshot{}, now)
	fact := result.Facts[0]
	if fact.Classification != hostfacts.ClassificationUnknownOwner || len(fact.ReasonCodes) == 0 || fact.Process.PID != nil {
		t.Fatalf("ambiguous fact = %#v", fact)
	}
}

func TestNormalizeRecognizesExactSystemdSSHSocketActivation(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	sshd := RawProcess{PID: 784, UID: 0, StartTime: "20", ExecutableToken: "/usr/sbin/sshd|123|456", SystemdUnit: "ssh.service"}
	systemd := RawProcess{PID: 1, UID: 0, StartTime: "1", ExecutableToken: "/usr/lib/systemd/systemd|789|012"}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Processes: []RawProcess{sshd, systemd}}}}, hostresources.ResourceSnapshot{}, now)
	fact := result.Facts[0]
	if fact.Process.PID == nil || *fact.Process.PID != sshd.PID || fact.Service.SystemdUnit != "ssh.service" || fact.ConfidenceBP != 9000 || containsReason(fact.ReasonCodes, "process_owner_ambiguous") || !containsReason(fact.ReasonCodes, "systemd_ssh_socket_activation_verified") {
		t.Fatalf("socket-activated SSH ownership was not exact: %#v", fact)
	}
}

func TestNormalizeRejectsLookalikeSystemdSSHSocketActivation(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	sshd := RawProcess{PID: 784, UID: 0, ExecutableToken: "/tmp/sshd|123|456", SystemdUnit: "ssh.service"}
	systemd := RawProcess{PID: 1, UID: 0, ExecutableToken: "/usr/lib/systemd/systemd|789|012"}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Processes: []RawProcess{sshd, systemd}}}}, hostresources.ResourceSnapshot{}, now)
	fact := result.Facts[0]
	if fact.Process.PID != nil || !containsReason(fact.ReasonCodes, "process_owner_ambiguous") || containsReason(fact.ReasonCodes, "systemd_ssh_socket_activation_verified") {
		t.Fatalf("lookalike socket activation was trusted: %#v", fact)
	}
}

func BenchmarkNormalize1000Facts(b *testing.B) {
	raw := PlatformSnapshot{Sockets: make([]RawSocket, 1000)}
	for i := range raw.Sockets {
		raw.Sockets[i] = RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: uint16(10000 + i), Inode: fmt.Sprint(i), Processes: []RawProcess{{PID: i + 1, UID: 1000, StartTime: fmt.Sprint(i), ExecutableToken: "fixture"}}}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := Normalize(raw, hostresources.ResourceSnapshot{}, time.Unix(1_000, 0))
		if len(result.Facts) != 1000 {
			b.Fatal(len(result.Facts))
		}
	}
}

func TestProviderUsesBoundedTypedPlatformSnapshot(t *testing.T) {
	provider := &Provider{Now: func() time.Time { return time.Unix(100, 0) }, Resources: func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }, ObservePlatform: func(_ context.Context, limits hostfacts.Limits) (PlatformSnapshot, error) {
		if limits.MaxSockets != 4096 || limits.MaxDecodedBytes != 4<<20 {
			t.Fatalf("limits = %#v", limits)
		}
		return PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv6, Bind: "::1", Port: 53}}}, nil
	}}
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	if err != nil || len(observation.Facts) != 1 {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
}
