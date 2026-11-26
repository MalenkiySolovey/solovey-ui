//go:build linux && !minimal

package blockcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"testing"

	rtnl "github.com/mdlayher/netlink"
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
	attrs := rtnl.NewAttributeEncoder()
	attrs.Uint32(unix.FRA_TABLE, unix.RT_TABLE_MAIN)
	attrs.Uint32(unix.FRA_SUPPRESS_PREFIXLEN, ^uint32(0))
	attrs.Uint32(unix.FRA_PRIORITY, 32766)
	attrs.Uint8(unix.FRA_PROTOCOL, unix.RTPROT_KERNEL)
	data, err := attrs.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []byte{unix.AF_INET, unix.AF_INET6} {
		rule := append([]byte{family, 0, 0, 0, unix.RT_TABLE_MAIN, 0, 0, unix.FR_ACT_TO_TBL, 0, 0, 0, 0}, data...)
		if !baselineRuleObservable(rule, family) {
			t.Fatalf("ordinary rule denied for family %d", family)
		}
	}
	for name, change := range map[string]func(*rtnl.AttributeEncoder, []byte){
		"port selector":  func(a *rtnl.AttributeEncoder, _ []byte) { a.Bytes(unix.FRA_DPORT_RANGE, []byte{0, 53, 0, 53}) },
		"socket mark":    func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint32(unix.FRA_FWMARK, 0) },
		"other table":    func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint32(unix.FRA_TABLE, 100) },
		"suppressed":     func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint32(unix.FRA_SUPPRESS_PREFIXLEN, 0) },
		"protocol":       func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint8(unix.FRA_IP_PROTO, unix.IPPROTO_TCP) },
		"unknown field":  func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint32(1234, 0) },
		"wrong encoding": func(a *rtnl.AttributeEncoder, _ []byte) { a.Uint8(unix.FRA_PRIORITY, 1) },
		"inverted":       func(_ *rtnl.AttributeEncoder, h []byte) { binary.NativeEndian.PutUint32(h[8:], unix.FIB_RULE_INVERT) },
		"rejected":       func(_ *rtnl.AttributeEncoder, h []byte) { h[7] = unix.FR_ACT_PROHIBIT },
		"action absent":  func(_ *rtnl.AttributeEncoder, h []byte) { h[7] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			rule := []byte{unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, 0, 0, unix.FR_ACT_TO_TBL, 0, 0, 0, 0}
			encoder := rtnl.NewAttributeEncoder()
			change(encoder, rule)
			attrs, err := encoder.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if baselineRuleObservable(append(rule, attrs...), unix.AF_INET) {
				t.Fatal("ambiguous routing policy was accepted")
			}
		})
	}
}

func TestBaselineReadsKernelRules(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_BASELINE_ISOLATED") != "1" {
		t.Skip("requires an isolated namespace with default policy rules")
	}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		if !baselineRoutingRulesObservable(context.Background(), family) {
			t.Errorf("ordinary kernel rules not observable for family %d", family)
		}
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
