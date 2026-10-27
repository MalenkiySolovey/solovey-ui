//go:build shtorm_extended && with_quic

package runtime

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/sudoku"
	M "github.com/sagernet/sing/common/metadata"
)

// Qualification peers use the selected engine SDK. They are not production
// outbounds or a second protocol implementation in Panel.
func sudokuPeer(t *testing.T, f *quicFixture, split, mode, multiplex string) adapter.Outbound {
	t.Helper()
	maskMode := mode
	if maskMode == "raw" {
		maskMode = "legacy"
	}
	raw, _ := json.Marshal(map[string]any{
		"server": "127.0.0.1", "server_port": f.port, "key": split,
		"aead_method": f.inbound["aead_method"], "padding_min": 0, "padding_max": 0,
		"table_type": "up_ascii_down_entropy", "enable_pure_downlink": false,
		"multiplex": multiplex, "http_mask": map[string]any{"enabled": mode != "raw", "mode": maskMode, "path_root": "owned"},
	})
	var options option.SudokuOutboundOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	ctx := f.core.instance.Context()
	logger := log.NewNOPFactory().Logger()
	peer, err := sudoku.NewOutbound(ctx, f.core.instance.Router(), logger, "sudoku-qualification", options)
	if err != nil {
		t.Fatal("selected Sudoku client construction failed", err)
	}
	lifecycle, ok := peer.(adapter.Lifecycle)
	if !ok {
		t.Fatal("selected Sudoku client has no serving lifecycle")
	}
	scope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error("Sudoku client cleanup failed", err)
		}
	})
	for _, stage := range adapter.ListStartStages {
		if err := scope.Start("sudoku-qualification", lifecycle, stage); err != nil {
			t.Fatal("Sudoku client start failed", err)
		}
	}
	return peer
}

func newSudokuFixture(t *testing.T, mode, aead string) (*quicFixture, string) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	master, err := credentials.GenerateSudokuMaster()
	if err != nil {
		t.Fatal(err)
	}
	public, _ := credentials.SudokuPublicKey(master)
	maskMode := mode
	if maskMode == "raw" {
		maskMode = "legacy"
	}
	f := &quicFixture{kind: "sudoku", core: NewCore(), port: port}
	f.inbound = map[string]any{"type": "sudoku", "tag": "quic-fixture", "listen": "127.0.0.1", "listen_port": port,
		"key": public, "aead_method": aead, "padding_min": 0, "padding_max": 0, "table_type": "up_ascii_down_entropy",
		"enable_pure_downlink": false, "handshake_timeout": 1, "disable_http_mask": mode == "raw", "http_mask_mode": maskMode, "path_root": "owned"}
	// The protocol has one serving key and no authenticated Users catalogue.
	if err := f.core.StartWithBindings(f.raw(), nil); err != nil {
		t.Fatal("Sudoku provider startup failed", err)
	}
	t.Cleanup(func() {
		if err := f.core.Stop(); err != nil {
			t.Error("Sudoku stop failed", err)
		}
	})
	f.echo = nativeTCPWitness(t)
	return f, master
}

func assertSudokuUnassociated(t *testing.T, f *quicFixture) ConnectionSnapshot {
	t.Helper()
	snapshot := f.parents(t, 0)
	if len(snapshot.Connections) == 0 || snapshot.ParentTotal != 0 {
		t.Fatal("Sudoku traffic observation missing or synthetic parent control exposed")
	}
	for _, flow := range snapshot.Connections {
		if flow.Inbound != "quic-fixture" || flow.InboundType != "sudoku" || flow.Identity != "unavailable" || flow.ClientID != 0 {
			t.Fatal("Sudoku delivery key invented authenticated Users identity")
		}
	}
	if f.parents(t, 7).Total != 0 || f.parents(t, 9).Total != 0 {
		t.Fatal("Sudoku client delivery split acquired Users accounting")
	}
	return snapshot
}

func retireSudokuInbound(t *testing.T, f *quicFixture) {
	t.Helper()
	if err := f.core.RemoveInbound("quic-fixture"); err != nil {
		t.Fatal("Sudoku owner removal failed", err)
	}
	f.core.CloseInboundConnections("quic-fixture")
	if snapshot := f.parents(t, 0); snapshot.Total != 0 || snapshot.ParentTotal != 0 {
		t.Fatal("Sudoku retirement retained a server-side flow")
	}
	probe, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
	if err != nil {
		t.Fatal("Sudoku retirement retained its listener", err)
	}
	_ = probe.Close()
}

func TestSudokuNativeProviderTrafficRestartAndSharedIdentity(t *testing.T) {
	for _, tc := range []struct{ mode, multiplex, aead string }{
		{"raw", "off", "none"}, {"legacy", "off", "chacha20-poly1305"},
		{"stream", "off", "aes-128-gcm"}, {"poll", "off", "chacha20-poly1305"},
		{"auto", "on", "chacha20-poly1305"}, {"ws", "on", "aes-128-gcm"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f, master := newSudokuFixture(t, tc.mode, tc.aead)
			first, _ := credentials.SplitSudokuMaster(master)
			second, _ := credentials.SplitSudokuMaster(master)
			if first == second {
				t.Fatal("independent delivery splits unexpectedly coincide")
			}
			peer := sudokuPeer(t, f, first, tc.mode, tc.multiplex)
			firstFlow := nativeStream(t, peer, f)
			secondFlow := nativeStream(t, sudokuPeer(t, f, second, tc.mode, tc.multiplex), f)
			nativeUDPWitness(t, peer)
			before := assertSudokuUnassociated(t, f)
			retireSudokuInbound(t, f)
			// HTTP tunnel peers persistently retry a remote outage. The server
			// owner must drain its flows and release its port; EOF at an external
			// retrying client is not that invariant. Retire these test peers too.
			for _, flow := range []net.Conn{firstFlow, secondFlow} {
				_ = flow.Close()
			}
			raw, _ := json.Marshal(f.inbound)
			if err := f.core.AddInboundWithBindings(raw, nil); err != nil {
				t.Fatal("Sudoku same-port reactivation failed", err)
			}
			reactivated := nativeStream(t, sudokuPeer(t, f, first, tc.mode, tc.multiplex), f)
			if err := f.core.Stop(); err != nil {
				t.Fatal(err)
			}
			probe, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
			if err != nil {
				t.Fatal("Sudoku runtime stop retained its listener", err)
			}
			_ = probe.Close()
			_ = reactivated.Close()
			if err := f.core.StartWithBindings(f.raw(), nil); err != nil {
				t.Fatal("Sudoku restart with retained serving identity failed", err)
			}
			_ = nativeStream(t, sudokuPeer(t, f, second, tc.mode, tc.multiplex), f)
			if after := assertSudokuUnassociated(t, f); after.Generation == before.Generation {
				t.Fatal("Sudoku runtime restart retained the old generation")
			}
		})
	}
}

func TestSudokuMasterRotationRevokesEveryOldSplit(t *testing.T) {
	f, master := newSudokuFixture(t, "raw", "chacha20-poly1305")
	first, _ := credentials.SplitSudokuMaster(master)
	second, _ := credentials.SplitSudokuMaster(master)
	oldFlows := []net.Conn{
		nativeStream(t, sudokuPeer(t, f, first, "raw", "off"), f),
		nativeStream(t, sudokuPeer(t, f, second, "raw", "off"), f),
	}
	retireSudokuInbound(t, f)
	rotated, _ := credentials.GenerateSudokuMaster()
	f.inbound["key"], _ = credentials.SudokuPublicKey(rotated)
	raw, _ := json.Marshal(f.inbound)
	if err := f.core.AddInboundWithBindings(raw, nil); err != nil {
		t.Fatal("Sudoku public identity replacement failed", err)
	}
	for _, flow := range oldFlows {
		if nativeEcho(flow) == nil {
			t.Fatal("master rotation retained an established old-key flow")
		}
	}
	for _, split := range []string{first, second} {
		peer := sudokuPeer(t, f, split, "raw", "off")
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		conn, err := peer.DialContext(ctx, "tcp", M.ParseSocksaddr(f.echo.Addr().String()))
		cancel()
		if err == nil {
			defer conn.Close()
			if nativeEcho(conn) == nil {
				t.Fatal("master rotation accepted an old delivery split")
			}
		}
	}
	current, _ := credentials.SplitSudokuMaster(rotated)
	_ = nativeStream(t, sudokuPeer(t, f, current, "raw", "off"), f)
	assertSudokuUnassociated(t, f)
}
