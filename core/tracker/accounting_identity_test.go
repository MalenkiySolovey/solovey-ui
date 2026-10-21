package tracker

import (
	"context"
	"net"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type accountingRouter struct {
	adapter.Router
	tracker *StatsTracker
}

func (r accountingRouter) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	wrapped := r.tracker.RoutedConnection(ctx, conn, metadata, nil, nil)
	_, err := wrapped.Write([]byte("sent"))
	_ = wrapped.Close()
	return err
}
func (r accountingRouter) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	wrapped := r.tracker.RoutedPacketConnection(ctx, conn, metadata, nil, nil)
	packet := buf.New()
	defer packet.Release()
	_, _ = packet.Write([]byte("sent"))
	err := wrapped.WritePacket(packet, M.ParseSocksaddr("198.51.100.1:443"))
	_ = wrapped.Close()
	return err
}

func TestAccountingUsesAcceptedPrincipalAcrossNamesAndInboundReplacement(t *testing.T) {
	for _, transport := range []string{"tcp", "packet"} {
		t.Run(transport, func(t *testing.T) {
			stats, owner := NewStatsTracker(), inboundidentity.NewOwner()
			defer owner.Close()
			metadata := adapter.InboundContext{User: "same-label"}
			var first adapter.Router
			for index, id := range []uint{7, 42} {
				tag := []string{"in-a", "in-b"}[index]
				routed, epoch, err := owner.Prepare(accountingRouter{tracker: stats}, tag, []inboundidentity.Binding{{Inbound: tag, Principal: metadata.User, ClientID: id}})
				if err != nil {
					t.Fatal(err)
				}
				owner.Publish(tag, epoch)
				metadata.Inbound = tag
				if transport == "tcp" {
					err = routed.RouteConnection(t.Context(), newBlockingTestConn(), metadata)
				} else {
					err = routed.RoutePacketConnection(t.Context(), &admissionPacketConn{}, metadata)
				}
				if err != nil {
					t.Fatal(err)
				}
				if index == 0 {
					first = routed
				}
			}
			// Replacement reuses the old label for a different user. Already accepted
			// traffic keeps its original identity; the retired parent cannot admit more.
			_, replacement, err := owner.Prepare(accountingRouter{tracker: stats}, "in-a", []inboundidentity.Binding{{Inbound: "in-a", Principal: "same-label", ClientID: 99}})
			if err != nil {
				t.Fatal(err)
			}
			owner.Publish("in-a", replacement)
			metadata.Inbound = "in-a"
			if err := first.RouteConnection(t.Context(), newBlockingTestConn(), metadata); err == nil {
				t.Fatal("retired inbound admitted a reused name")
			}
			actual := map[entitytypes.VPNUserID]int64{}
			for _, sample := range stats.GetStats() {
				if sample.Resource == "user" {
					actual[sample.VPNUserID] += sample.Traffic
				}
			}
			if len(actual) != 2 || actual[7] != 4 || actual[42] != 4 {
				t.Fatalf("identity changed at drain: %#v", actual)
			}
		})
	}
}

func TestAccountingRestoreKeepsIdentityAndConcurrentAcceptedBytes(t *testing.T) {
	stats := NewStatsTracker()
	before := Stat{Resource: "user", Tag: "old-name", VPNUserID: 7, Direction: true, Traffic: 10}
	stats.RestoreStats([]Stat{before})
	drained := stats.GetStats()
	// Accepted concurrent bytes and another user's reused label must both survive
	// a failed persistence retry, without name resolution at restore time.
	stats.RestoreStats([]Stat{{Resource: "user", Tag: "old-name", VPNUserID: 7, Direction: true, Traffic: 3}, {Resource: "user", Tag: "old-name", VPNUserID: 42, Direction: true, Traffic: 5}})
	stats.RestoreStats(drained)
	actual := map[entitytypes.VPNUserID]int64{}
	for _, sample := range stats.GetStats() {
		actual[sample.VPNUserID] += sample.Traffic
	}
	if actual[7] != 13 || actual[42] != 5 {
		t.Fatalf("retry changed attribution: %#v", actual)
	}
}
