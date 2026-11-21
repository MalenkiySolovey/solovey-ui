//go:build linux && !minimal && zapretproof

package planner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
// for each enabled family and the namespace's IPv4/IPv6 routing rules. The
// broker calls it in the socket namespace. It
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
	revisions := [2]struct{ Default, Rules string }{}
	for i, family := range []struct {
		observe bool
		id      byte
		gateway *netip.Addr
	}{{ipv4, unix.AF_INET, &egress.IPv4Gateway}, {ipv6, unix.AF_INET6, &egress.IPv6Gateway}} {
		if family.observe {
			*family.gateway, revisions[i].Default, err = zapretDefaultRoute(ctx, conn, ifindex, family.id)
			if err != nil {
				return ZapretEgress{}, fmt.Errorf("observe Zapret gateway (family %d, ifindex %d): %w", family.id, ifindex, err)
			}
		}
		// The guard reserves the same marks in both families, including a
		// family with no admitted gateway. Inventory both routing policies.
		bits, policy, err := zapretRoutingRules(ctx, conn, family.id)
		if err != nil {
			return ZapretEgress{}, fmt.Errorf("observe Zapret routing policy (family %d): %w", family.id, err)
		}
		egress.RoutingMarkBits |= bits
		revisions[i].Rules = policy
	}
	egress.RouteRevision = revision(revisions)
	return egress, ctx.Err()
}

func zapretDefaultRoute(ctx context.Context, conn *socket.Conn, ifindex int32, family byte) (netip.Addr, string, error) {
	var gateway netip.Addr
	var identity string
	header := make([]byte, unix.SizeofRtMsg)
	header[0], header[4] = family, unix.RT_TABLE_MAIN
	if err := zapretRTNLDump(ctx, conn, uint32(family), unix.RTM_GETROUTE, unix.RTM_NEWROUTE, header, func(row []byte) error {
		addr, snapshot, err := parseZapretDefaultRoute(row, ifindex, family)
		if err == nil && addr.IsValid() {
			if gateway.IsValid() {
				return errors.New("multiple main-table defaults are not supported")
			}
			gateway, identity = addr, snapshot
		}
		return err
	}); err != nil {
		return netip.Addr{}, "", err
	}
	if !gateway.IsValid() {
		return netip.Addr{}, "", errors.New("no main-table default gateway")
	}
	return gateway, identity, nil
}

func zapretRTNLDump(ctx context.Context, conn *socket.Conn, seq uint32, requestType, rowType uint16, header []byte, consume func([]byte) error) error {
	// Receive the terminal NLMSG_DONE ourselves: the ordinary netlink Conn
	// strips it, including NLM_F_DUMP_INTR on a concurrently modified FIB.
	request := netlink.Message{Header: netlink.Header{Length: uint32(unix.SizeofNlMsghdr + len(header)),
		Type: netlink.HeaderType(requestType), Flags: netlink.Request | netlink.Dump, Sequence: seq}, Data: header}
	data, err := request.MarshalBinary()
	if err != nil {
		return err
	}
	if err := conn.Sendto(ctx, data, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	var buffer [32 << 10]byte
	for count := 0; count < 8192; {
		n, _, flags, from, err := conn.Recvmsg(ctx, buffer[:], nil, 0)
		if err != nil {
			return err
		}
		peer, ok := from.(*unix.SockaddrNetlink)
		if !ok || peer.Pid != 0 || flags&unix.MSG_TRUNC != 0 {
			return errors.New("truncated or non-kernel route inventory")
		}
		rows, err := syscall.ParseNetlinkMessage(buffer[:n])
		if err != nil || len(rows) == 0 {
			return errors.New("invalid route inventory response")
		}
		for i, row := range rows {
			count++
			if count > 8192 || row.Header.Seq != seq || row.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return errors.New("route inventory exceeded its bound or changed during observation")
			}
			switch row.Header.Type {
			case unix.NLMSG_DONE:
				if i != len(rows)-1 || len(row.Data) < 4 || binary.NativeEndian.Uint32(row.Data[:4]) != 0 {
					return errors.New("route inventory did not complete")
				}
				return nil
			case rowType:
				if err := consume(row.Data); err != nil {
					return err
				}
			default:
				return errors.New("unexpected route inventory response")
			}
		}
	}
	return errors.New("route inventory exceeded its bound")
}

func zapretRoutingRules(ctx context.Context, conn *socket.Conn, family byte) (uint32, string, error) {
	const fibRuleHeaderSize = 12 // Linux UAPI struct fib_rule_hdr.
	header := make([]byte, fibRuleHeaderSize)
	header[0] = family
	var reserved uint32
	hash := sha256.New()
	if err := zapretRTNLDump(ctx, conn, uint32(family)+256, unix.RTM_GETRULE, unix.RTM_NEWRULE, header, func(row []byte) error {
		if len(row) < fibRuleHeaderSize || row[0] != family {
			return errors.New("unexpected routing rule family")
		}
		attrs, err := netlink.NewAttributeDecoder(row[fibRuleHeaderSize:])
		if err != nil {
			return err
		}
		var mark, mask uint32
		var hasMark, hasMask bool
		for attrs.Next() {
			switch attrs.Type() {
			case unix.FRA_FWMARK:
				if hasMark {
					return errors.New("duplicate routing rule mark")
				}
				mark, hasMark = attrs.Uint32(), true
			case unix.FRA_FWMASK:
				if hasMask {
					return errors.New("duplicate routing rule mark mask")
				}
				mask, hasMask = attrs.Uint32(), true
			}
		}
		if err := attrs.Err(); err != nil {
			return err
		}
		if mark != 0 && !hasMask {
			mask = ^uint32(0)
		}
		reserved |= mark | mask
		// Rule order is routing semantics, including ties in priority.
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(row)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(row)
		return nil
	}); err != nil {
		return 0, "", err
	}
	return reserved, hex.EncodeToString(hash.Sum(nil)), nil
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
