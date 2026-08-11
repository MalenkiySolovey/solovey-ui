package resourceinventory

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type fakeSettings struct {
	listen, domain, path, cert, key string
	port                            int
	subListen, subDomain, subPath   string
	subCert, subKey                 string
	subPort                         int
}

func (s fakeSettings) GetListen() (string, error)      { return s.listen, nil }
func (s fakeSettings) GetPort() (int, error)           { return s.port, nil }
func (s fakeSettings) GetWebDomain() (string, error)   { return s.domain, nil }
func (s fakeSettings) GetWebPath() (string, error)     { return s.path, nil }
func (s fakeSettings) GetCertFile() (string, error)    { return s.cert, nil }
func (s fakeSettings) GetKeyFile() (string, error)     { return s.key, nil }
func (s fakeSettings) GetSubListen() (string, error)   { return s.subListen, nil }
func (s fakeSettings) GetSubPort() (int, error)        { return s.subPort, nil }
func (s fakeSettings) GetSubDomain() (string, error)   { return s.subDomain, nil }
func (s fakeSettings) GetSubPath() (string, error)     { return s.subPath, nil }
func (s fakeSettings) GetSubCertFile() (string, error) { return s.subCert, nil }
func (s fakeSettings) GetSubKeyFile() (string, error)  { return s.subKey, nil }

func TestPanelContributorClassifiesWithoutExposingAdminPath(t *testing.T) {
	settings := fakeSettings{listen: "127.0.0.1", port: 2095, domain: "panel.example", path: "/private-admin-token/", cert: "cert.pem", key: "key.pem"}
	items, err := (panelContributor{settings: settings}).ListProtectableResources(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("ListProtectableResources: items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ID != "core:panel:web" || item.Public || !item.TLS || item.Capabilities.CanServeFallback != hostresources.CapabilityYes {
		t.Fatalf("panel resource = %#v", item)
	}
	payload, _ := json.Marshal(item)
	if strings.Contains(string(payload), "private-admin-token") {
		t.Fatalf("admin path leaked into inventory: %s", payload)
	}
}

func TestSubscriptionContributorUsesStableSafeIdentity(t *testing.T) {
	settings := fakeSettings{subListen: "0.0.0.0", subPort: 8443, subDomain: "sub.example", subPath: "/subscription/secret-path", subCert: "cert.pem", subKey: "key.pem"}
	first, err := (subscriptionContributor{settings: settings}).ListProtectableResources(t.Context())
	if err != nil || len(first) != 1 {
		t.Fatalf("ListProtectableResources: items=%#v err=%v", first, err)
	}
	item := first[0]
	if item.ID != "core:subscription:default" || item.Listen != "0.0.0.0" || item.Port != 8443 || item.Protocol != "http" || !item.Public || !item.TLS {
		t.Fatalf("subscription resource = %#v", item)
	}
	if item.Capabilities.CanServeFallback != hostresources.CapabilityNo || item.Capabilities.SupportsGracefulDrain != hostresources.CapabilityUnknown {
		t.Fatalf("subscription capabilities changed unknown into a boolean: %#v", item.Capabilities)
	}
	payload, _ := json.Marshal(item)
	if strings.Contains(string(payload), "secret-path") {
		t.Fatalf("subscription path leaked into inventory: %s", payload)
	}

	settings.subPath = "/subscription/another-secret"
	settings.subDomain = "SUB.EXAMPLE."
	second, err := (subscriptionContributor{settings: settings}).ListProtectableResources(t.Context())
	if err != nil || len(second) != 1 {
		t.Fatalf("updated subscription inventory: items=%#v err=%v", second, err)
	}
	if second[0].ID != item.ID {
		t.Fatalf("safe display/config change altered subscription identity: %q -> %q", item.ID, second[0].ID)
	}
}

func TestInboundResourceUsesStableIDAndSafeListenerFacts(t *testing.T) {
	snapshot := inventorySnapshot(17, "trojan", "tcp", coreinboundcontrol.CapabilitySupported)
	snapshot.Tag = "renamable"
	snapshot.TLS.Enabled = true
	snapshot.Listener.ProxyProtocol = true
	item := inboundResource(snapshot)
	if item.ID != "core:inbound:17" || item.Protocol != "stream" || item.Port != 443 || !item.Public {
		t.Fatalf("inbound resource = %#v", item)
	}
	if item.Capabilities.AcceptsProxyProtocol != hostresources.CapabilityYes || item.Capabilities.CanServeFallback != hostresources.CapabilityYes {
		t.Fatalf("inbound capabilities = %#v", item.Capabilities)
	}
	payload, _ := json.Marshal(item)
	if strings.Contains(string(payload), "password") {
		t.Fatalf("raw inbound options leaked: %s", payload)
	}
}

func TestInboundResourcePreservesNeutralAuthenticationFacts(t *testing.T) {
	snapshot := inventorySnapshot(101, "http", "tcp", coreinboundcontrol.CapabilityUnsupported)
	snapshot.Authentication = coreinboundcontrol.AuthenticationShapeV1{Known: true, Expected: true}
	item := inboundResource(snapshot)
	if item.Endpoints[0].AuthenticationExpected != hostresources.CapabilityYes {
		t.Fatalf("authenticated HTTP endpoint = %#v", item.Endpoints[0])
	}
	snapshot.Authentication.Expected = false
	item = inboundResource(snapshot)
	if item.Endpoints[0].AuthenticationExpected != hostresources.CapabilityNo {
		t.Fatalf("anonymous HTTP endpoint = %#v", item.Endpoints[0])
	}
}

func TestCapabilityUnknownAndFalseRemainDistinct(t *testing.T) {
	udp := inboundResource(inventorySnapshot(18, "hysteria2", "udp", coreinboundcontrol.CapabilityOutOfScope))
	if udp.Capabilities.CanServeFallback != hostresources.CapabilityNo {
		t.Fatalf("UDP fallback capability = %q, want explicit no", udp.Capabilities.CanServeFallback)
	}
	if udp.Capabilities.SupportsGracefulDrain != hostresources.CapabilityUnknown || udp.Capabilities.RequiresACMEHTTP01 != hostresources.CapabilityUnknown {
		t.Fatalf("unknown capabilities were collapsed into false: %#v", udp.Capabilities)
	}
}

func TestEveryCurrentInboundYieldsTypedEndpointOrExplicitUnknown(t *testing.T) {
	types := []string{"direct", "http", "mixed", "naive", "shadowtls", "shadowsocks", "socks", "trojan", "vless", "vmess", "anytls", "hysteria", "hysteria2", "tuic", "tun", "redirect", "tproxy", "future-unknown"}
	for index, inboundType := range types {
		t.Run(inboundType, func(t *testing.T) {
			network := "tcp"
			switch inboundType {
			case "hysteria", "hysteria2", "tuic":
				network = "udp"
			case "mixed", "shadowsocks", "socks":
				network = "tcp_udp"
			case "direct", "tun", "redirect", "tproxy", "future-unknown":
				network = "unknown"
			}
			item := inboundResource(inventorySnapshot(uint(index+1), inboundType, network, coreinboundcontrol.CapabilityUnknown))
			if len(item.Endpoints) == 0 {
				t.Fatal("inbound yielded no endpoint facts")
			}
			for _, endpoint := range item.Endpoints {
				if endpoint.Schema != hostresources.EndpointSchemaV1 || endpoint.ResourceID != item.ID {
					t.Fatalf("endpoint identity = %#v", endpoint)
				}
				if !endpoint.Known() && len(endpoint.ReasonCodes) == 0 {
					t.Fatalf("unknown endpoint has no reason: %#v", endpoint)
				}
			}
		})
	}
}

func TestDualNetworkInboundKeepsTCPAndUDPSeparate(t *testing.T) {
	snapshot := inventorySnapshot(99, "mixed", "tcp_udp", coreinboundcontrol.CapabilityUnsupported)
	snapshot.Listener.Port = 1080
	item := inboundResource(snapshot)
	if len(item.Endpoints) != 2 || item.Endpoints[0].Key.Network == item.Endpoints[1].Key.Network {
		t.Fatalf("mixed endpoint facts = %#v", item.Endpoints)
	}
}

func TestTrojanTypeAloneDoesNotClaimFallback(t *testing.T) {
	snapshot := inventorySnapshot(100, "trojan", "tcp", coreinboundcontrol.CapabilityUnknown)
	item := inboundResource(snapshot)
	if item.Capabilities.CanServeFallback != hostresources.CapabilityUnknown || item.Endpoints[0].FallbackSupported != hostresources.CapabilityUnknown {
		t.Fatalf("unproven Trojan capability = %#v", item)
	}
}

func inventorySnapshot(id uint, inboundType, network string, disposition coreinboundcontrol.CapabilityDisposition) coreinboundcontrol.InboundFallbackSnapshotV1 {
	return coreinboundcontrol.InboundFallbackSnapshotV1{
		Schema:            coreinboundcontrol.InboundSnapshotSchemaV1,
		InboundDatabaseID: id,
		ResourceID:        "core:inbound:" + strconv.FormatUint(uint64(id), 10),
		Tag:               inboundType + "-tag",
		Type:              inboundType,
		Listener: coreinboundcontrol.ListenerShapeV1{
			Network: network, AddressFamily: "ipv4", Bind: "0.0.0.0", Port: 443,
		},
		InboundOptionsDigest:  strings.Repeat("a", 64),
		ConfigurationRevision: strings.Repeat("c", 64),
		Capability: coreinboundcontrol.NativeFallbackCapabilityV1{
			Disposition: disposition, CapabilityResolverRevision: coreinboundcontrol.CapabilityResolverRevisionV1,
		},
	}
}
