//go:build with_quic

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestStopRetiresPendingOutboundDial(t *testing.T) {
	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blackhole.Close()
	raw, err := json.Marshal(map[string]any{
		"log": map[string]any{"disabled": true},
		"outbounds": []any{map[string]any{"type": "hysteria2", "tag": "pending", "server": "127.0.0.1",
			"server_port": blackhole.LocalAddr().(*net.UDPAddr).Port, "password": "local-fixture",
			"tls": map[string]any{"enabled": true, "insecure": true, "server_name": "localhost"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := NewCore()
	t.Cleanup(func() {
		if err := client.Stop(); err != nil {
			t.Error(err)
		}
	})
	for generation := 0; generation < 2; generation++ {
		if err := client.Start(raw); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		dialDone := make(chan error, 1)
		go func() {
			conn, err := client.DialOutbound(ctx, "pending", "tcp", M.ParseSocksaddr("192.0.2.1:443"))
			if conn != nil {
				_ = conn.Close()
			}
			dialDone <- err
		}()
		_ = blackhole.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := blackhole.ReadFrom(make([]byte, 2048)); err != nil {
			cancel()
			<-dialDone
			t.Fatal("native QUIC handshake did not start:", err)
		}
		stopped := make(chan error, 1)
		go func() { stopped <- client.Stop() }()
		select {
		case err := <-stopped:
			if err != nil {
				cancel()
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			cancel()
			<-dialDone
			<-stopped
			t.Fatal("Core.Stop waited for the caller to cancel its pending outbound dial")
		}
		select {
		case err := <-dialDone:
			if !errors.Is(err, ErrCoreUnavailable) {
				cancel()
				t.Fatal("retired dial error:", err)
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatal("Core.Stop returned before joining its admitted dial")
		}
		cancel()
		if _, err := client.DialOutbound(context.Background(), "pending", "tcp", M.ParseSocksaddr("192.0.2.1:443")); !errors.Is(err, ErrCoreUnavailable) {
			t.Fatal("stopped core admitted a dial:", err)
		}
	}
}
