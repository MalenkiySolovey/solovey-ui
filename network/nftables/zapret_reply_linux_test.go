//go:build linux && !minimal

package nftables

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func TestZapretReplyRulesShareOnlySelectedTCPQueue(t *testing.T) {
	revision := strings.Repeat("1", 64)
	host := hostref.Local()
	request := intent.ZapretPacketPathIntent{
		Schema: intent.ZapretPacketPathSchema, HostRef: host, Owner: intent.ZapretOwner,
		ResourceRef: "outbound:direct", OutboundRef: "direct", StrategyRef: "circular",
		StrategyRevision: revision, DesiredRevision: revision, InputRevision: revision,
		Protocols: []intent.PacketProtocol{intent.PacketProtocolTCP, intent.PacketProtocolUDP},
		TCPPorts:  []uint16{80, 443, 853}, UDPPorts: []uint16{443}, Purpose: intent.ZapretPurpose,
		Directions: []intent.PacketDirection{intent.PacketDirectionOutbound, intent.PacketDirectionInbound}, NeedsReturn: true,
	}
	facts := planner.Facts{
		HostRef: host, Revision: revision, CapabilityRevision: revision, InventoryComplete: true,
		Capability: deployment.PlatformFact{Availability: deployment.Available, Backend: planner.ZapretSystemdBackend},
		Zapret: &planner.ZapretFacts{
			OutboundRef: "direct", OutboundRevision: revision, Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true,
			InstalledEgress: &planner.ZapretEgress{NamespaceCookie: 11, IfIndex: 2, RouteRevision: revision,
				IPv4Gateway: netip.MustParseAddr("192.0.2.1"), IPv6Gateway: netip.MustParseAddr("2001:db8::1")},
			SystemdSerial: 7, SystemdReplyGuard: true, SystemdFamilies: &planner.ZapretFamilies{
				IPv4: planner.ZapretPorts{TCP: []uint16{80, 443}, UDP: []uint16{443}, ReplyTCP: []uint16{80}},
				IPv6: planner.ZapretPorts{TCP: []uint16{853}, ReplyTCP: []uint16{853}},
			},
		},
	}
	plan, err := planner.CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := buildZapretBatch([]planner.ZapretPlan{plan}, nil)
	if err != nil || draft.chains != 4 {
		t.Fatalf("two live input hooks are required: %d %v", draft.chains, err)
	}
	queueRules, passthrough, verify := 0, 0, 0
	for _, message := range draft.messages {
		if message.Header.Type != zapretNFTType(unix.NFT_MSG_NEWRULE) {
			continue
		}
		chain, expressions := zapretReplyRuleExpressions(t, message)
		switch chain {
		case zapretReplyQueue:
			if !expressions["socket"] || !expressions["ct"] || !expressions["payload"] {
				t.Fatal("reply rule lacks socket, direction or remote port evidence")
			}
			if expressions["queue"] {
				queueRules++
			} else {
				passthrough++
			}
		case zapretReplyVerify:
			if !expressions["socket"] || expressions["queue"] {
				t.Fatal("post-queue verification is not bound to the original socket")
			}
			verify++
		}
	}
	if queueRules != 2 || passthrough != 1 || verify != 1 {
		t.Fatalf("expected two family-specific queue rules and one static TCP rule: %d/%d/%d", queueRules, passthrough, verify)
	}
	old, oldChains := zapretDenyShape("boot", nil, false)
	retained, retainedChains := zapretDenyShape("boot", nil, true)
	if bytes.Equal(old, retained) || oldChains != 1 || retainedChains != 3 {
		t.Fatal("old outgoing-only deny could be mistaken for retained reply denial")
	}
}

func zapretReplyRuleExpressions(t *testing.T, message netlink.Message) (string, map[string]bool) {
	t.Helper()
	attrs, err := netlink.NewAttributeDecoder(message.Data[4:])
	if err != nil {
		t.Fatal(err)
	}
	var chain string
	expressions := make(map[string]bool)
	for attrs.Next() {
		switch attrs.Type() {
		case unix.NFTA_RULE_CHAIN:
			chain = attrs.String()
		case unix.NFTA_RULE_EXPRESSIONS:
			attrs.Nested(func(list *netlink.AttributeDecoder) error {
				for list.Next() {
					list.Nested(func(fields *netlink.AttributeDecoder) error {
						for fields.Next() {
							if fields.Type() == unix.NFTA_EXPR_NAME {
								expressions[fields.String()] = true
							}
						}
						return fields.Err()
					})
				}
				return list.Err()
			})
		}
	}
	if err := attrs.Err(); err != nil {
		t.Fatal(err)
	}
	return chain, expressions
}
