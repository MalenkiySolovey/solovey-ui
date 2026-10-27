//go:build shtorm_extended && with_quic

package runtime

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/bond"
	"github.com/sagernet/sing-box/protocol/socks"
)

func bondSocksOptions(port int) *option.SOCKSOutboundOptions {
	return &option.SOCKSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(port)}, Version: "5", Username: "verified-7", Password: quicFixturePassword}
}

func TestBondMemberIdentityTrafficAndOwnedReload(t *testing.T) {
	f := &quicFixture{kind: "bond", core: NewCore()}
	ports := []int{quicPort(t), quicPort(t)}
	members := make([]any, 0, 2)
	bindings := make([]inboundidentity.Binding, 0, 2)
	for i, port := range ports {
		tag := "bond-member-" + strconv.Itoa(i)
		members = append(members, map[string]any{"type": "socks", "tag": tag, "listen": "127.0.0.1", "listen_port": port, "users": []any{map[string]any{"username": "verified-7", "password": quicFixturePassword}}})
		bindings = append(bindings, inboundidentity.Binding{Inbound: tag, Principal: "verified-7", ClientID: 7})
	}
	f.inbound = map[string]any{"type": "bond", "tag": "bond-root", "inbounds": members}
	if err := f.core.StartWithBindings(f.raw(), bindings); err != nil {
		t.Fatal("start native group", err)
	}
	t.Cleanup(func() { _ = f.core.Stop() })
	f.echo = nativeTCPWitness(t)
	ctx := f.core.instance.Context()
	logger := log.NewNOPFactory().Logger()
	ordinary, err := socks.NewOutbound(ctx, f.core.instance.Router(), logger, "ordinary-member-peer", *bondSocksOptions(ports[0]))
	if err != nil {
		t.Fatal(err)
	}
	ordinaryStream := nativeStream(t, ordinary, f)
	known := f.parents(t, 7)
	if len(known.Connections) != 1 || known.Connections[0].Inbound != "bond-member-0" || known.Connections[0].Identity != "verified" {
		t.Fatal("ordinary member flow lost its canonical Users identity")
	}
	options := option.BondOutboundOptions{Outbounds: []option.BondOutbound{
		{Outbound: option.Outbound{Type: "socks", Tag: "peer-0", Options: bondSocksOptions(ports[0])}, DownloadRatio: 35, UploadRatio: 70},
		{Outbound: option.Outbound{Type: "socks", Tag: "peer-1", Options: bondSocksOptions(ports[1])}, DownloadRatio: 65, UploadRatio: 30},
	}}
	peer, err := bond.NewOutbound(ctx, f.core.instance.Router(), logger, "bond-peer", options)
	if err != nil {
		t.Fatal(err)
	}
	scope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() { _ = scope.Close() })
	for _, stage := range adapter.ListStartStages {
		if err := scope.Start("bond-peer", peer.(adapter.Lifecycle), stage); err != nil {
			t.Fatal(err)
		}
	}
	bondedStream := nativeStream(t, peer, f)
	nativeUDPWitness(t, peer)
	snapshot := f.parents(t, 0)
	sawGroup := false
	for _, flow := range snapshot.Connections {
		if flow.Inbound == "bond-root" {
			sawGroup = true
			if flow.ClientID != 0 || flow.Identity != "unavailable" {
				t.Fatal("group invented a per-client authenticated identity")
			}
		}
	}
	if !sawGroup {
		t.Fatal("bonded traffic did not reach the existing runtime inventory")
	}
	for _, tag := range []string{"bond-root", "bond-member-0", "bond-member-1"} {
		available, _, actual, digest, _, present := f.core.LookupInboundExact(tag)
		if !available || !present || actual != tag || digest == "" {
			t.Fatal("actual group/member effective identity is missing", tag)
		}
	}
	if err := f.core.RemoveInbound("bond-root"); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"bond-root", "bond-member-0", "bond-member-1"} {
		f.core.CloseInboundConnections(tag)
		if _, _, _, present := f.core.LookupInbound(tag); present {
			t.Fatal("retired member remained in effective runtime", tag)
		}
	}
	if nativeEcho(ordinaryStream) == nil || nativeEcho(bondedStream) == nil {
		t.Fatal("owned group reload left accepted traffic alive")
	}
	for _, port := range ports {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal("group did not release a member port", err)
		}
		_ = listener.Close()
	}
	raw, _ := json.Marshal(f.inbound)
	if err := f.core.AddInboundWithBindings(raw, bindings); err != nil {
		t.Fatal("group reapply failed", err)
	}
	nativeStream(t, ordinary, f)
	nativeStream(t, peer, f)
}
