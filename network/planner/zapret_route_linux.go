//go:build linux && !minimal

package planner

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"syscall"
	"time"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/socket"
	"golang.org/x/sys/unix"
)

// ObserveZapretLinuxEgress snapshots the single gateway route in table main
// for each enabled family. The broker calls it in the socket namespace. It
// does not infer the effective path for arbitrary destinations: the adapter
// must also check the packet's actual output device and next hop in kernel.
func ObserveZapretLinuxEgress(ctx context.Context, ifindex int32, ipv4, ipv6 bool) (ZapretEgress, error) {
	if ifindex <= 0 || !ipv4 && !ipv6 {
		return ZapretEgress{}, errors.New("egress observation for Zapret requires a selected link and address family")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := socket.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE, "zapret-route", nil)
	if err != nil {
		return ZapretEgress{}, err
	}
	defer conn.Close()
	if err := conn.Bind(&unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return ZapretEgress{}, err
	}
	if err := conn.SetsockoptInt(unix.SOL_NETLINK, unix.NETLINK_GET_STRICT_CHK, 1); err != nil {
		return ZapretEgress{}, err
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return ZapretEgress{}, err
	}
	var cookie uint64
	var cookieErr error
	if err := raw.Control(func(fd uintptr) {
		cookie, cookieErr = unix.GetsockoptUint64(int(fd), unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	}); err != nil || cookieErr != nil || cookie == 0 {
		return ZapretEgress{}, errors.Join(errors.New("could not prove namespace for Zapret route observation"), err, cookieErr)
	}
	egress := ZapretEgress{NamespaceCookie: cookie, IfIndex: ifindex}
	revisions := [2]string{}
	for i, family := range []struct {
		observe bool
		id      byte
		gateway *netip.Addr
	}{{ipv4, unix.AF_INET, &egress.IPv4Gateway}, {ipv6, unix.AF_INET6, &egress.IPv6Gateway}} {
		if !family.observe {
			continue
		}
		*family.gateway, revisions[i], err = zapretDefaultRoute(ctx, conn, ifindex, family.id)
		if err != nil {
			return ZapretEgress{}, fmt.Errorf("observe Zapret gateway (family %d, ifindex %d): %w", family.id, ifindex, err)
		}
	}
	egress.RouteRevision = revision(revisions)
	return egress, ctx.Err()
}

func zapretDefaultRoute(ctx context.Context, conn *socket.Conn, ifindex int32, family byte) (netip.Addr, string, error) {
	// Receive the terminal NLMSG_DONE ourselves: the ordinary netlink Conn
	// strips it, including NLM_F_DUMP_INTR on a concurrently modified FIB.
	seq := uint32(family)
	request := netlink.Message{Header: netlink.Header{Length: unix.SizeofNlMsghdr + unix.SizeofRtMsg,
		Type: unix.RTM_GETROUTE, Flags: netlink.Request | netlink.Dump, Sequence: seq}, Data: make([]byte, unix.SizeofRtMsg)}
	request.Data[0], request.Data[4] = family, unix.RT_TABLE_MAIN
	data, err := request.MarshalBinary()
	if err != nil {
		return netip.Addr{}, "", err
	}
	if err := conn.Sendto(ctx, data, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return netip.Addr{}, "", err
	}
	var gateway netip.Addr
	var identity string
	var buffer [32 << 10]byte
	for count := 0; count < 8192; {
		n, _, flags, from, err := conn.Recvmsg(ctx, buffer[:], nil, 0)
		if err != nil {
			return netip.Addr{}, "", err
		}
		peer, ok := from.(*unix.SockaddrNetlink)
		if !ok || peer.Pid != 0 || flags&unix.MSG_TRUNC != 0 {
			return netip.Addr{}, "", errors.New("truncated or non-kernel route inventory")
		}
		rows, err := syscall.ParseNetlinkMessage(buffer[:n])
		if err != nil || len(rows) == 0 {
			return netip.Addr{}, "", errors.New("invalid route inventory response")
		}
		for i, row := range rows {
			count++
			if count > 8192 || row.Header.Seq != seq || row.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return netip.Addr{}, "", errors.New("route inventory exceeded its bound or changed during observation")
			}
			switch row.Header.Type {
			case unix.NLMSG_DONE:
				if i != len(rows)-1 || len(row.Data) < 4 || binary.NativeEndian.Uint32(row.Data[:4]) != 0 || !gateway.IsValid() {
					return netip.Addr{}, "", errors.New("no complete inventory with a single main-table default gateway")
				}
				return gateway, identity, nil
			case unix.RTM_NEWROUTE:
				addr, snapshot, err := parseZapretDefaultRoute(row.Data, ifindex, family)
				if err != nil {
					return netip.Addr{}, "", err
				}
				if addr.IsValid() {
					if gateway.IsValid() {
						return netip.Addr{}, "", errors.New("multiple main-table defaults are not supported")
					}
					gateway, identity = addr, snapshot
				}
			default:
				return netip.Addr{}, "", errors.New("unexpected route inventory response")
			}
		}
	}
	return netip.Addr{}, "", errors.New("route inventory exceeded its bound")
}

func parseZapretDefaultRoute(data []byte, ifindex int32, family byte) (netip.Addr, string, error) {
	if len(data) < unix.SizeofRtMsg || data[0] != family {
		return netip.Addr{}, "", errors.New("unexpected family in route dump")
	}
	if data[1] != 0 { // rtm_dst_len: only defaults, not device-local subnets
		return netip.Addr{}, "", nil
	}
	if data[2] != 0 || data[3] != 0 || data[7] != unix.RTN_UNICAST ||
		binary.NativeEndian.Uint32(data[8:12])&^uint32(unix.RTNH_F_ONLINK) != 0 {
		return netip.Addr{}, "", errors.New("default route is not a supported unicast gateway")
	}
	attrs, err := netlink.NewAttributeDecoder(data[unix.SizeofRtMsg:])
	if err != nil {
		return netip.Addr{}, "", err
	}
	var gateway netip.Addr
	var oif, table uint32
	table = uint32(data[4])
	seen := make(map[uint16]bool)
	var stable []netlink.Attribute
	for attrs.Next() {
		typ := attrs.Type()
		if seen[typ] {
			return netip.Addr{}, "", errors.New("duplicate default route attribute")
		}
		seen[typ] = true
		switch typ {
		case unix.RTA_GATEWAY:
			gateway, _ = netip.AddrFromSlice(attrs.Bytes())
		case unix.RTA_OIF:
			oif = attrs.Uint32()
		case unix.RTA_TABLE:
			table = attrs.Uint32()
		case unix.RTA_PRIORITY, unix.RTA_PREFSRC, unix.RTA_PREF, unix.RTA_METRICS:
		case unix.RTA_CACHEINFO, unix.RTA_EXPIRES:
			// Kernel countdowns are not the identity of the route. Expiry
			// removes the route; a different gateway is blocked per packet.
			continue
		default:
			return netip.Addr{}, "", fmt.Errorf("unsupported default route attribute %d", typ)
		}
		stable = append(stable, netlink.Attribute{Type: typ, Data: attrs.Bytes()})
	}
	if attrs.Err() != nil || table != unix.RT_TABLE_MAIN || oif != uint32(ifindex) || !zapretGateway(gateway) ||
		family == unix.AF_INET && !gateway.Is4() || family == unix.AF_INET6 && (!gateway.Is6() || gateway.Is4In6()) {
		return netip.Addr{}, "", errors.New("default gateway does not use the selected link and family in table main")
	}
	slices.SortFunc(stable, func(a, b netlink.Attribute) int { return int(a.Type) - int(b.Type) })
	return gateway, revision(struct {
		Header []byte
		Attrs  []netlink.Attribute
	}{data[:unix.SizeofRtMsg], stable}), nil
}
