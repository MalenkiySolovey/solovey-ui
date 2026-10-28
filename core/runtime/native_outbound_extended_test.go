//go:build shtorm_extended && with_quic

package runtime

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
)

// Exercise the production registry and Scope rather than constructing a
// qualification-only outbound beside the running core.
func TestNativeOutboundRegisteredTrafficAndRestart(t *testing.T) {
	for _, fixture := range []struct{ kind, transport string }{{"mieru", "TCP"}, {"trusttunnel", "tcp"}, {"trusttunnel", "udp"}, {"sudoku", "raw"}, {"sudoku", "poll"}} {
		t.Run(fixture.kind+"/"+fixture.transport, func(t *testing.T) {
			var remote *quicFixture
			var sudokuKey string
			if fixture.kind == "mieru" {
				remote = newNativeMieruFixture(t, fixture.transport)
			} else if fixture.kind == "sudoku" {
				var master string
				remote, master = newSudokuFixture(t, fixture.transport, "chacha20-poly1305")
				var err error
				sudokuKey, err = credentials.SplitSudokuMaster(master)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				remote = newQUICFixture(t, fixture.kind)
			}
			options := map[string]any{"type": fixture.kind, "tag": "native-client", "server": "127.0.0.1", "server_port": remote.port, "username": "verified-7", "password": quicFixturePassword}
			if fixture.kind == "mieru" {
				options["transport"], options["multiplexing"], options["handshake_mode"] = fixture.transport, "MULTIPLEXING_LOW", "HANDSHAKE_STANDARD"
			} else if fixture.kind == "sudoku" {
				delete(options, "username")
				delete(options, "password")
				options["key"], options["aead_method"] = sudokuKey, remote.inbound["aead_method"]
				options["padding_min"], options["padding_max"] = 0, 0
				options["table_type"], options["enable_pure_downlink"] = "up_ascii_down_entropy", false
				options["multiplex"] = "off"
				if fixture.transport == "poll" {
					options["multiplex"] = "on"
				}
				options["http_mask"] = map[string]any{"enabled": fixture.transport != "raw", "mode": "poll", "path_root": "owned"}
			} else {
				options["quic"] = fixture.transport == "udp"
				options["health_check"] = true
				options["multiplex"] = map[string]any{"enabled": true, "max_connections": 2, "max_streams": 8}
				options["tls"] = map[string]any{"enabled": true, "server_name": "quic-fixture.invalid", "certificate": remote.inbound["tls"].(map[string]any)["certificate"]}
			}
			raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "outbounds": []any{options}})
			client := NewCore()
			t.Cleanup(func() {
				if err := client.Stop(); err != nil {
					t.Error("native client cleanup", err)
				}
			})
			for generation := 0; generation < 2; generation++ {
				if err := client.Start(raw); err != nil {
					t.Fatal("native registered outbound start", err)
				}
				peer, ok := client.instance.Outbound().Outbound("native-client")
				if !ok {
					t.Fatal("production registry lost the native outbound")
				}
				stream := nativeStream(t, peer, remote)
				nativeUDPWitness(t, peer)
				if fixture.kind == "sudoku" {
					assertSudokuUnassociated(t, remote)
				} else {
					for _, flow := range remote.parents(t, 7).Connections {
						if flow.ClientID != 7 || flow.Identity != "verified" {
							t.Fatal("native client authentication lost its remote owner")
						}
					}
				}
				if err := client.Stop(); err != nil {
					t.Fatal("native client stop", err)
				}
				if nativeEcho(stream) == nil {
					t.Fatal("retired native client kept forwarding")
				}
				_ = stream.Close()
			}
		})
	}
}
