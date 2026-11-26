//go:build linux && !minimal

package blockcheck

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"time"

	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/operationref"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/sagernet/netlink"
	"golang.org/x/sys/unix"
)

func hostBaselineAvailable(ctx context.Context) bool {
	_, err := newHostBaseline(ctx)
	return err == nil
}

func openHostBaseline(ctx context.Context, host hostref.Ref, selection feature.PathSelection) (feature.Path, error) {
	if selection.Ref != "" || selection.Resolver != "" {
		return feature.Path{}, feature.ErrInvalidRun
	}
	baseline, err := newHostBaseline(ctx)
	if err != nil {
		return feature.Path{}, err
	}
	return feature.Path{
		Evidence: feature.PathEvidence{Host: host, Mode: feature.HostBaseline, Scope: "ordinary-host-socket", Context: "host-system-dns"},
		Current:  baseline.current,
		Resolve:  baseline.resolve,
		DialTCP:  baseline.dialTarget("tcp"),
		DialUDP:  baseline.dialTarget("udp"),
	}, nil
}

type hostBaseline struct {
	client    *broker.Client
	namespace uint64
}

func newHostBaseline(ctx context.Context) (hostBaseline, error) {
	client := broker.NewClient(broker.RolePanel)
	observed, err := observeBaseline(ctx, client)
	if err != nil {
		return hostBaseline{}, err
	}
	baseline := hostBaseline{client: client, namespace: observed.Namespace}
	// Compare with an unsent, unprivileged socket before advertising the path.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return hostBaseline{}, feature.ErrBaselineUnobservable
	}
	defer unix.Close(fd)
	if !baseline.socketObservable(fd) {
		return hostBaseline{}, feature.ErrBaselineUnobservable
	}
	return baseline, ctx.Err()
}

func (b hostBaseline) current(ctx context.Context) error {
	observed, err := observeBaseline(ctx, b.client)
	if err != nil {
		return err
	}
	if observed.Namespace != b.namespace {
		return feature.ErrBaselineUnobservable
	}
	return nil
}

func observeBaseline(ctx context.Context, client *broker.Client) (broker.HostBaselineObservation, error) {
	var result broker.HostBaselineObservation
	_, err := client.Invoke(ctx, broker.Call{Verb: broker.VerbHostBaseline, OperationID: "blockcheck-host-baseline", Timeout: time.Second, Payload: struct{}{}}, &result)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil || result.Namespace == 0 {
		return result, feature.ErrBaselineUnobservable
	}
	return result, nil
}

func (b hostBaseline) dialTarget(network string) func(context.Context, netip.AddrPort) (net.Conn, feature.SocketEvidence, error) {
	return func(ctx context.Context, target netip.AddrPort) (net.Conn, feature.SocketEvidence, error) {
		if !permitTarget(target.Addr()) {
			return nil, feature.SocketEvidence{}, feature.ErrTargetRejected
		}
		return b.dial(ctx, network, target)
	}
}

func (b hostBaseline) resolve(ctx context.Context, name string, family feature.Family, probe feature.Probe) (feature.Resolution, error) {
	if probe != feature.DNS {
		return feature.Resolution{}, feature.ErrResolverUnsupported
	}
	var (
		mu        sync.Mutex
		observed  feature.SocketEvidence
		uncertain bool
	)
	// Use the standard resolver's configured numeric nameserver. A local stub
	// would hide its own upstream socket; a lookup with no observed DNS dial
	// (for example from /etc/hosts) is not positive baseline DNS evidence.
	resolver := net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := netip.ParseAddrPort(address)
		if err != nil || target.Port() != 53 {
			return nil, feature.ErrBaselineUnobservable
		}
		conn, observation, err := b.dial(ctx, network, target)
		mu.Lock()
		if !observation.OutsidePanel || observed.OutsidePanel && observation.Interface != observed.Interface {
			uncertain = true
		}
		observed = observation
		mu.Unlock()
		return conn, err
	}}
	lookupFamily := "ip"
	if family == feature.IPv4 {
		lookupFamily = "ip4"
	} else if family == feature.IPv6 {
		lookupFamily = "ip6"
	}
	addrs, err := resolver.LookupNetIP(ctx, lookupFamily, name+".")
	mu.Lock()
	defer mu.Unlock()
	if uncertain || !observed.OutsidePanel {
		return feature.Resolution{}, feature.ErrBaselineUnobservable
	}
	return feature.Resolution{Addresses: addrs, Context: "host-system-dns", Socket: observed}, err
}

func (b hostBaseline) dial(ctx context.Context, network string, target netip.AddrPort) (net.Conn, feature.SocketEvidence, error) {
	addr := target.Addr()
	if network != "tcp" && network != "udp" || !addr.IsGlobalUnicast() || addr.Zone() != "" || addr.Is4In6() {
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	if addr.Is4() {
		network += "4"
	} else {
		network += "6"
	}
	handle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	defer handle.Close()
	if err := handle.SetSocketTimeout(250 * time.Millisecond); err != nil {
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	before, err := baselineLookup(handle, addr, netip.Addr{})
	if err != nil {
		return nil, feature.SocketEvidence{}, err
	}
	dialer := net.Dialer{ControlContext: b.inspectSocket}
	conn, err := dialer.DialContext(ctx, network, target.String())
	if err != nil {
		// The network path is not independently observed for a failed socket.
		return nil, feature.SocketEvidence{}, err
	}
	socket, ok := conn.(syscall.Conn)
	if !ok {
		_ = conn.Close()
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	raw, err := socket.SyscallConn()
	if err != nil || b.inspectSocket(ctx, "", "", raw) != nil {
		_ = conn.Close()
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	local, err := netip.ParseAddrPort(conn.LocalAddr().String())
	if err != nil || local.Addr() != before.source || conn.RemoteAddr().String() != target.String() {
		_ = conn.Close()
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	after, err := baselineLookup(handle, addr, local.Addr())
	if err != nil || after != before || ctx.Err() != nil {
		_ = conn.Close()
		return nil, feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	return conn, feature.SocketEvidence{Interface: after.name, OutsidePanel: true}, nil
}

func (b hostBaseline) inspectSocket(ctx context.Context, _, _ string, conn syscall.RawConn) error {
	var valid bool
	err := conn.Control(func(fd uintptr) {
		valid = b.socketObservable(int(fd))
	})
	if err != nil || !valid {
		return feature.ErrBaselineUnobservable
	}
	return ctx.Err()
}

func (b hostBaseline) socketObservable(fd int) bool {
	namespace, nsErr := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	mark, markErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK)
	device, devErr := unix.GetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE)
	return nsErr == nil && namespace != 0 && namespace == b.namespace && markErr == nil && mark == 0 && devErr == nil && device == ""
}

type baselineRoute struct {
	name    string
	index   int
	source  netip.Addr
	gateway netip.Addr
}

func baselineLookup(handle *netlink.Handle, addr, source netip.Addr) (baselineRoute, error) {
	family := unix.AF_INET6
	if addr.Is4() {
		family = unix.AF_INET
	}
	rules, err := handle.RuleList(family)
	if err != nil || !baselineRoutingRulesObservable(rules) {
		return baselineRoute{}, feature.ErrBaselineUnobservable
	}
	uid := uint32(os.Geteuid())
	options := &netlink.RouteGetOptions{UID: &uid}
	if source.IsValid() {
		options.SrcAddr = source.AsSlice()
	}
	routes, err := handle.RouteGetWithOptions(addr.AsSlice(), options)
	if err != nil || len(routes) != 1 {
		return baselineRoute{}, feature.ErrBaselineUnobservable
	}
	route := routes[0]
	if route.Type != unix.RTN_UNICAST || route.Table != unix.RT_TABLE_MAIN || route.LinkIndex <= 0 || len(route.MultiPath) != 0 || route.Encap != nil || route.NewDst != nil || route.Via != nil {
		return baselineRoute{}, feature.ErrBaselineUnobservable
	}
	// A lookup with the connected local address need not echo a preferred
	// source. When it does, require it to agree with the supplied source.
	routeSource := source
	if route.Src != nil {
		observed, _ := netip.AddrFromSlice(route.Src.IP)
		if source.IsValid() && observed.Unmap() != source {
			return baselineRoute{}, feature.ErrBaselineUnobservable
		}
		routeSource = observed.Unmap()
	}
	if !routeSource.IsGlobalUnicast() {
		return baselineRoute{}, feature.ErrBaselineUnobservable
	}
	link, err := handle.LinkByIndex(route.LinkIndex)
	if err != nil || link == nil || !baselineLinkObservable(link) {
		return baselineRoute{}, feature.ErrBaselineUnobservable
	}
	gateway, _ := netip.AddrFromSlice(route.Gw)
	return baselineRoute{name: link.Attrs().Name, index: route.LinkIndex, source: routeSource.Unmap(), gateway: gateway.Unmap()}, nil
}

// In the initial scope a kernel lookup without a transport tuple is only
// used when policy rules cannot select a different route for that tuple.
func baselineRoutingRulesObservable(rules []netlink.Rule) bool {
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		if rule.Type != unix.FR_ACT_TO_TBL || (rule.Table != unix.RT_TABLE_MAIN && rule.Table != unix.RT_TABLE_LOCAL && rule.Table != unix.RT_TABLE_DEFAULT) ||
			rule.Src.IsValid() || rule.Dst.IsValid() || rule.MarkSet || rule.Mask != -1 || rule.Tos != 0 || rule.TunID != 0 || rule.Goto != -1 || rule.Flow != -1 ||
			rule.IifName != "" || rule.OifName != "" || rule.SuppressIfgroup != -1 || rule.SuppressPrefixlen != -1 || rule.Invert ||
			rule.Dport != nil || rule.Sport != nil || rule.IPProto != 0 || rule.UIDRange != nil {
			return false
		}
	}
	return true
}

func baselineLinkObservable(link netlink.Link) bool {
	attrs := link.Attrs()
	if attrs == nil || attrs.Flags&(net.FlagUp|net.FlagLoopback) != net.FlagUp || !operationref.ValidToken(attrs.Name, 64) {
		return false
	}
	switch link.Type() {
	case "device", "veth", "bond", "bridge", "vlan", "macvlan", "ipvlan":
		return true
	default:
		return false
	}
}
