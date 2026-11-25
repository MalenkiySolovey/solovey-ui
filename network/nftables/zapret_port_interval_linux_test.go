//go:build linux && !minimal && zapretproof

package nftables

import (
	"net"
	"os"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	knft "github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// Use the same encoded range comparison as the owned batch, on a private
// loopback only. The counter cannot alter or admit any selected traffic.
func TestZapretPortIntervalOnLoopback(t *testing.T) {
	parent := os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if parent == "" {
		t.Skip("requires an isolated loopback namespace")
	}
	if _, err := probeNamespace(parent); err != nil {
		t.Fatal(err)
	}
	const start, end = 40000, 46000
	conn := &knft.Conn{}
	table := conn.AddTable(&knft.Table{Name: "solovey_interval_proof", Family: knft.TableFamilyINet})
	chain := conn.AddChain(&knft.Chain{Name: "outbound", Table: table, Type: knft.ChainTypeFilter,
		Hooknum: knft.ChainHookOutput, Priority: knft.ChainPriorityFilter})
	conn.AddRule(&knft.Rule{Table: table, Chain: chain, Exprs: []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_UDP}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
		zapretPortMatch(1, intent.PortInterval{First: start, Last: end}),
		&expr.Counter{},
	}})
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.DelTable(table)
		if err := conn.Flush(); err != nil {
			t.Error(err)
		}
	})
	for _, family := range []struct {
		network string
		ip      net.IP
	}{{"udp4", net.IPv4(127, 0, 0, 1)}, {"udp6", net.IPv6loopback}} {
		for _, port := range []int{start - 1, start, 43000, end, end + 1} {
			packet, err := net.DialUDP(family.network, nil, &net.UDPAddr{IP: family.ip, Port: port})
			if err != nil {
				t.Fatal(err)
			}
			_, err = packet.Write([]byte{0})
			_ = packet.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	rules, err := conn.GetRules(table, chain)
	if err != nil || len(rules) != 1 {
		t.Fatalf("one interval rule expected, got %d: %v", len(rules), err)
	}
	for _, action := range rules[0].Exprs {
		if count, ok := action.(*expr.Counter); ok {
			if count.Packets != 6 {
				t.Fatalf("range includes adjacent port or misses an inclusive end: %d packets, want 6", count.Packets)
			}
			return
		}
	}
	t.Fatal("missing range match counter")
}
