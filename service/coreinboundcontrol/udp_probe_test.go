package coreinboundcontrol

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"gorm.io/gorm"
)

type identityProbeHydrator struct{}

func (identityProbeHydrator) HydrateInbound(_ context.Context, _ *gorm.DB, _ *model.Inbound, content []byte) ([]byte, error) {
	return append([]byte(nil), content...), nil
}

func TestPlainUDPProbeProviderPerformsBoundedShadowsocksRequestResponse(t *testing.T) {
	port := reserveUDPPort(t)
	password := "fixture-secret-password"
	config := []byte(strings.ReplaceAll(`{"log":{"disabled":true},"inbounds":[{"type":"shadowsocks","tag":"udp-probe-in","listen":"127.0.0.1","listen_port":PORT,"network":"udp","method":"aes-128-gcm","password":"PASSWORD"}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`, "PORT", strconv.Itoa(port)))
	config = []byte(strings.ReplaceAll(string(config), "PASSWORD", password))
	ctx := sb.Context(t.Context(), registry.InboundRegistry(), registry.OutboundRegistry(), registry.EndpointRegistry(), registry.DNSTransportRegistry(), registry.ServiceRegistry())
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, config); err != nil {
		t.Fatal(err)
	}
	instance, err := corebox.NewBox(corebox.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if err = instance.Start(); err != nil {
		_ = instance.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })

	inbound := model.Inbound{Id: 71, Type: "shadowsocks", Tag: "udp-probe-in", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":` + strconv.Itoa(port) + `,"network":"udp","method":"aes-128-gcm","password":"` + password + `"}`)}
	fixture := newPatchFixture(t, inbound)
	fixture.service.mutation.Hydrator = identityProbeHydrator{}
	var persisted model.Inbound
	if err = fixture.db.First(&persisted, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	content, err := persisted.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := canonicalInboundOptionsDigest(t.Context(), content)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.observation.OptionsDigest = digest
	fixture.service.effective = exactEffectiveReader{typ: inbound.Type, tag: inbound.Tag, digest: digest}
	snapshot, err := fixture.service.Snapshot(t.Context(), inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	revision := hostresources.Revision("udp-probe-binding")
	target := componenthealth.ProtocolProbeTargetV1{ResourceID: snapshot.ResourceID, EndpointID: "claim:udp-probe", ProtocolClass: hostresources.TransportPlainUDP,
		RuntimeRevision: snapshot.Effective.Revision, CapabilityRevision: revision, ConfigurationRevision: snapshot.ConfigurationRevision, SocketRevision: revision,
		AddressFamily: hostresources.AddressFamilyIPv4, ConfiguredBind: "127.0.0.1", Port: uint16(port)}
	provider := NewPlainUDPProbeProviderV1(fixture.service)
	registry := componenthealth.NewProtocolProbeRegistryV1()
	if _, err = registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	capability := registry.Capability(t.Context(), target)
	if !capability.Available {
		t.Fatalf("plain UDP provider unavailable: %#v", capability)
	}
	observation, err := registry.ProbeFresh(t.Context(), componenthealth.ProtocolProbeRequestV1{Target: target, ContributionRevision: revision, CompositionRevision: revision,
		ManagedPlanRevision: revision, ProviderInstance: capability.ProviderInstance, NotBeforeUnixNano: time.Now().Add(-time.Millisecond).UnixNano()})
	if err != nil {
		t.Fatalf("probe target=%#v capability=%#v: %v", target, capability, err)
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Passed || !observation.RequestResponse || !observation.ExactTarget || strings.Contains(string(encoded), password) || observation.StartedUnixNano <= 0 || observation.CompletedUnixNano < observation.StartedUnixNano {
		t.Fatalf("unsafe or incomplete observation: %s", encoded)
	}
}

func reserveUDPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
