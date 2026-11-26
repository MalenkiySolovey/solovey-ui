//go:build linux && !minimal

package blockcheck

import (
	"errors"
	"net"
	"testing"

	"github.com/sagernet/netlink"
	"golang.org/x/sys/unix"
)

func TestBaselineChecksSocketNamespace(t *testing.T) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	identity, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if errors.Is(err, unix.ENOPROTOOPT) {
		t.Skip("kernel lacks socket namespace cookies")
	}
	if err != nil || identity == 0 {
		t.Fatalf("read socket namespace: %d, %v", identity, err)
	}
	if !(hostBaseline{namespace: identity}).socketObservable(fd) {
		t.Fatal("an ordinary unbound socket in the attested namespace was denied")
	}
	if (hostBaseline{namespace: identity + 1}).socketObservable(fd) {
		t.Fatal("a socket in a different namespace was accepted")
	}
}

func TestBaselineRejectsUnobservedRoutePolicy(t *testing.T) {
	ordinary := *netlink.NewRule()
	ordinary.Type, ordinary.Table = unix.FR_ACT_TO_TBL, unix.RT_TABLE_MAIN
	if !baselineRoutingRulesObservable([]netlink.Rule{ordinary}) {
		t.Fatal("ordinary rule denied")
	}
	for name, change := range map[string]func(*netlink.Rule){
		"port selector": func(rule *netlink.Rule) { rule.Dport = netlink.NewRulePortRange(53, 53) },
		"socket mark":   func(rule *netlink.Rule) { rule.MarkSet = true },
		"other table":   func(rule *netlink.Rule) { rule.Table = 100 },
		"suppressed":    func(rule *netlink.Rule) { rule.SuppressPrefixlen = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			rule := ordinary
			change(&rule)
			if baselineRoutingRulesObservable([]netlink.Rule{ordinary, rule}) {
				t.Fatal("ambiguous routing policy was accepted")
			}
		})
	}
}

func TestBaselineRejectsUnobservedLink(t *testing.T) {
	attrs := netlink.LinkAttrs{Name: "test-egress", Flags: net.FlagUp}
	if !baselineLinkObservable(&netlink.Device{LinkAttrs: attrs}) {
		t.Fatal("ordinary link denied")
	}
	if baselineLinkObservable(&netlink.Tuntap{LinkAttrs: attrs}) {
		t.Fatal("a TUN alone does not prove the flow avoids Solovey")
	}
	attrs.Flags |= net.FlagLoopback
	if baselineLinkObservable(&netlink.Device{LinkAttrs: attrs}) {
		t.Fatal("loopback cannot attest a resolver's upstream")
	}
}
