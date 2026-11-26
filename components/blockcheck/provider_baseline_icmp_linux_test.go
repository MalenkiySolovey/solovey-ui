//go:build linux && !minimal

package blockcheck

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"golang.org/x/net/icmp"
)

func TestHostEchoReplyMatchesOnlyTheRequest(t *testing.T) {
	for _, dst := range []string{"198.51.100.2", "2001:db8::2"} {
		t.Run(dst, func(t *testing.T) {
			p := echoEndpoint(netip.MustParseAddr(dst))
			nonce := []byte("fixture-nonce")
			for _, tc := range []struct {
				message icmp.Message
				want    bool
			}{
				{icmp.Message{Type: p.reply, Body: &icmp.Echo{ID: 73, Seq: echoSeq, Data: nonce}}, true},
				{icmp.Message{Type: p.reply, Body: &icmp.Echo{ID: 72, Seq: echoSeq, Data: nonce}}, false},
				{icmp.Message{Type: p.reply, Body: &icmp.Echo{ID: 73, Seq: echoSeq + 1, Data: nonce}}, false},
				{icmp.Message{Type: p.reply, Body: &icmp.Echo{ID: 73, Seq: echoSeq, Data: []byte("another-nonce")}}, false},
				{icmp.Message{Type: p.request, Body: &icmp.Echo{ID: 73, Seq: echoSeq, Data: nonce}}, false},
			} {
				wire, err := tc.message.Marshal(nil)
				if err != nil || p.matchesReply(wire, 73, nonce) != tc.want {
					t.Fatalf("message %+v: matches want %t, marshal error %v", tc.message, tc.want, err)
				}
			}
		})
	}
}

func TestHostEchoCaplessLoopbackFixture(t *testing.T) {
	mode := os.Getenv("SOLOVEY_TEST_CAPLESS_ICMP")
	if mode != "allow" && mode != "deny" {
		t.Skip("requires an isolated Linux namespace with only loopback")
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil || os.Geteuid() == 0 || !strings.Contains(string(data), "CapBnd:\t0000000000000000\n") ||
		!strings.Contains(string(data), "CapEff:\t0000000000000000\n") {
		t.Fatalf("fixture must have a non-root UID and no capabilities: %v", err)
	}
	for _, addr := range []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.IPv6Loopback()} {
		t.Run(addr.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			probe := echoEndpoint(addr)
			conn, err := probe.open(ctx)
			if conn != nil {
				defer conn.Close()
			}
			if mode == "deny" {
				if !errors.Is(err, feature.ErrICMPEchoUnavailable) || conn != nil {
					t.Fatalf("denied Echo socket = %v, %v", conn, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			local, err := conn.Getsockname()
			source, id := echoAddress(local)
			if err != nil || source != addr || id == 0 {
				t.Fatalf("socket address = %v, %d, %v", source, id, err)
			}
			if err := probe.exchange(ctx, conn, addr, id); err != nil {
				t.Fatal(err)
			}
		})
	}
}
