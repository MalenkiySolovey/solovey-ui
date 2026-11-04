//go:build shtorm_extended && with_quic

package runtime

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	"github.com/sagernet/sing-box/adapter"
	corelog "github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
)

func TestNativeLimiterOutboundsTrafficAndRestart(t *testing.T) {
	echo := nativeTCPWitness(t)
	for _, kind := range []string{"bandwidth-limiter", "connection-limiter", "rate-limiter", "traffic-limiter"} {
		t.Run(kind, func(t *testing.T) {
			client := NewCore()
			t.Cleanup(func() { _ = client.Stop() })
			limit := map[string]any{"type": kind, "tag": "limit", "route": map[string]any{"final": "direct"}, "strategy": "global", "mode": "bidirectional"}
			switch kind {
			case "bandwidth-limiter":
				limit["speed"] = "10MB"
			case "traffic-limiter":
				limit["total"] = "1MB"
			case "connection-limiter":
				limit["strategy"], limit["count"] = "connection", 2
				delete(limit, "mode")
			case "rate-limiter":
				limit["strategy"], limit["count"], limit["interval"] = "token-bucket", 10, "1s"
				delete(limit, "mode")
			}
			raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}, limit}})
			for generation := 0; generation < 2; generation++ {
				if err := client.Start(raw); err != nil {
					t.Fatal("native limiter startup", err)
				}
				dialContext, cancel := context.WithCancel(t.Context())
				conn, err := client.DialOutbound(dialContext, "limit", "tcp", M.ParseSocksaddr(echo.Addr().String()))
				cancel()
				if err != nil {
					t.Fatal("native limiter dial", err)
				}
				if err := nativeEcho(conn); err != nil {
					t.Fatal("ordinary TCP echo through native limiter", err)
				}
				_ = conn.Close()
				peer, _ := client.instance.Outbound().Outbound("limit")
				// ListenPacket is the native adapter API; it receives normal
				// routed metadata, as the production Core dial port now does.
				ctx, metadata := adapter.ExtendContext(corelog.ContextWithNewID(t.Context()))
				metadata.Network = "udp"
				nativeUDPWitness(t, peer, ctx)
				if err := client.Stop(); err != nil {
					t.Fatal("native limiter stop", err)
				}
			}
		})
	}
}

func TestNativeOutboundGroupsRegisteredTrafficAndRestart(t *testing.T) {
	for _, kind := range []string{"bond", "failover"} {
		t.Run(kind, func(t *testing.T) {
			remote := &quicFixture{kind: kind, core: NewCore()}
			var members []any
			var bindings []inboundidentity.Binding
			ports := []int{quicPort(t), quicPort(t)}
			for i, port := range ports {
				tag := "remote-leg-" + strconv.Itoa(i)
				members = append(members, map[string]any{"type": "socks", "tag": tag, "listen": "127.0.0.1", "listen_port": port, "users": []any{map[string]any{"username": "verified-7", "password": quicFixturePassword}}})
				bindings = append(bindings, inboundidentity.Binding{Inbound: tag, Principal: "verified-7", ClientID: 7})
			}
			remote.inbound = map[string]any{"type": kind, "tag": "remote-group", "inbounds": members}
			if err := remote.core.StartWithBindings(remote.raw(), bindings); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = remote.core.Stop() })
			remote.echo = nativeTCPWitness(t)
			client := NewCore()
			t.Cleanup(func() { _ = client.Stop() })
			for generation := 0; generation < 2; generation++ {
				var options []any
				var canonical []any
				var cut func()
				for i, port := range ports {
					if i == 0 && kind == "failover" {
						port, cut = nativeGroupCutProxy(t, port)
					}
					tag := "client-leg-" + strconv.Itoa(i)
					canonical = append(canonical, map[string]any{"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": port, "username": "verified-7", "password": quicFixturePassword})
					member := map[string]any{"type": "selector", "outbounds": []string{tag}, "default": tag}
					if kind == "bond" {
						ratio := 35
						if i == 1 {
							ratio = 65
						}
						options = append(options, map[string]any{"outbound": member, "download_ratio": ratio, "upload_ratio": 100 - ratio})
					} else {
						options = append(options, member)
					}
				}
				group := map[string]any{"type": kind, "tag": "native-group", "outbounds": options}
				raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "outbounds": append(canonical, group)})
				if err := client.Start(raw); err != nil {
					t.Fatal("start registered native group", err)
				}
				peer, ok := client.instance.Outbound().Outbound("native-group")
				if !ok {
					t.Fatal("production registry lost native group")
				}
				stream := nativeStream(t, peer, remote)
				nativeUDPWitness(t, peer)
				if cut != nil {
					cut()
					if err := nativeEcho(stream); err != nil {
						t.Fatal("native group did not resume through remaining member", err)
					}
				}
				member, _ := client.instance.Outbound().Outbound("client-leg-1")
				if err := client.RemoveOutbound("native-group"); err != nil {
					t.Fatal("remove native parent", err)
				}
				if nativeEcho(stream) == nil {
					t.Fatal("removed parent kept forwarding through borrowed members")
				}
				_ = stream.Close()
				retained, ok := client.instance.Outbound().Outbound("client-leg-1")
				if !ok || retained != member {
					t.Fatal("removing native parent retired its canonical member")
				}
				groupRaw, _ := json.Marshal(group)
				if err := client.AddOutbound(groupRaw); err != nil {
					t.Fatal("recreate parent over retained canonical members", err)
				}
				peer, _ = client.instance.Outbound().Outbound("native-group")
				stream = nativeStream(t, peer, remote)
				if err := client.Stop(); err != nil {
					t.Fatal("stop registered native group", err)
				}
				if nativeEcho(stream) == nil {
					t.Fatal("retired native group kept forwarding")
				}
				_ = stream.Close()
			}
		})
	}
}
