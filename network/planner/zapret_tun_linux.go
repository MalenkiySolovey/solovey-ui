//go:build linux && !minimal

package planner

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/socket"
	"golang.org/x/sys/unix"
)

// ZapretTUNName belongs to the optional installed ingress profile. The
// retained kernel deny must fence this device even after its broker exits.
const ZapretTUNName = zapretactivation.TUNInterface

// ZapretTUNForwardBit is reserved only by the installed TUN profile, and is
// not a socket mark or an input to any routing policy rule.
const ZapretTUNForwardBit uint32 = 1 << 28

// ZapretTUNScope is provisioned by the system administrator, not Panel JSON.
// The preceding lookup and terminal prohibit rules must already exist before
// Product can admit this ingress. They persist independently of the broker.
type ZapretTUNScope struct {
	Ingress     string       `json:"ingress"`
	Client      netip.Addr   `json:"client"`
	Destination netip.Prefix `json:"destination"`
	Table       uint32       `json:"table"`
	Priority    uint32       `json:"priority"`
}

func (s ZapretTUNScope) Revision(bootID string) string {
	return revision(struct {
		Scope ZapretTUNScope
		Boot  string
	}{s, bootID})
}

func (s ZapretTUNScope) valid() bool {
	if s.Ingress == "" || s.Ingress == "lo" || s.Ingress == ZapretTUNName || len(s.Ingress) >= unix.IFNAMSIZ {
		return false
	}
	for _, c := range s.Ingress {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return s.Client.Is4() && s.Client.IsGlobalUnicast() && s.Destination.Addr().Is4() &&
		s.Destination.Addr().IsGlobalUnicast() && s.Destination.Bits() >= 16 && s.Destination == s.Destination.Masked() &&
		!s.Destination.Contains(s.Client) && s.Table >= 256 && s.Table <= 1<<31-1 && s.Priority > 0 && s.Priority < 32765
}

// CheckZapretTUNScope confirms that nothing before the terminal rule can
// divert this scope to ordinary forwarding. Refuse unknown early selectors
// rather than trying to infer whether they match. Only kernel-local addresses
// can be handled by the preceding standard local rule.
func CheckZapretTUNScope(ctx context.Context, s ZapretTUNScope, egress int) (int, error) {
	if !s.valid() || egress <= 0 {
		return 0, errors.New("invalid installed Zapret TUN scope")
	}
	in, err := net.InterfaceByName(s.Ingress)
	if err != nil || in.Index == egress || in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
		return 0, errors.New("the reserved Zapret TUN ingress is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := socket.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE, "zapret-tun-policy", nil)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if err := conn.Bind(&unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, err
	}
	header := make([]byte, 12) // struct fib_rule_hdr
	header[0] = unix.AF_INET
	var found uint8
	err = zapretRTNLDump(ctx, conn, 1, unix.RTM_GETRULE, unix.RTM_NEWRULE, header, func(row []byte) error {
		mask, err := classifyTUNPolicy(row, s)
		if err != nil || found&mask != 0 {
			return errors.New("conflicting IPv4 rules precede the Zapret TUN prohibit")
		}
		found |= mask
		return nil
	})
	if err != nil {
		return 0, err
	}
	if found != 7 {
		return 0, errors.New("the exact lookup and terminal prohibit rules must be installed for Zapret TUN")
	}
	return in.Index, ctx.Err()
}

func classifyTUNPolicy(row []byte, s ZapretTUNScope) (uint8, error) {
	invalid := errors.New("unexpected IPv4 TUN routing rule")
	if len(row) < 12 || row[0] != unix.AF_INET {
		return 0, invalid
	}
	a, err := netlink.NewAttributeDecoder(row[12:])
	if err != nil {
		return 0, err
	}
	var source, destination netip.Addr
	var name string
	table, priority := uint32(row[4]), uint32(0)
	var extra bool
	seen := make(map[uint16]bool)
	for a.Next() {
		key := a.Type()
		if seen[key] {
			return 0, invalid
		}
		seen[key] = true
		switch key {
		case unix.FRA_PRIORITY:
			priority = a.Uint32()
		case unix.FRA_TABLE:
			table = a.Uint32()
		case unix.FRA_SRC:
			source, _ = netip.AddrFromSlice(a.Bytes())
		case unix.FRA_DST:
			destination, _ = netip.AddrFromSlice(a.Bytes())
		case unix.FRA_IIFNAME:
			name = a.String()
		case unix.FRA_PROTOCOL: // origin, not a selector
			_ = a.Uint8()
		case unix.FRA_SUPPRESS_PREFIXLEN:
			extra = extra || a.Uint32() != ^uint32(0)
		default:
			extra = true
		}
	}
	if a.Err() != nil {
		return 0, invalid
	}
	// No other rule can select the private table, even at a later priority.
	if priority > s.Priority+1 && table != s.Table {
		return 0, nil
	}
	flags := binary.NativeEndian.Uint32(row[8:12])
	if extra || row[3] != 0 || flags != 0 {
		return 0, invalid
	}
	if priority == 0 && table == unix.RT_TABLE_LOCAL && row[7] == unix.FR_ACT_TO_TBL &&
		row[1] == 0 && row[2] == 0 && name == "" && !seen[unix.FRA_SRC] && !seen[unix.FRA_DST] {
		return 1, nil
	}
	if row[2] != 32 || row[1] != byte(s.Destination.Bits()) || name != s.Ingress || source != s.Client || destination != s.Destination.Addr() {
		return 0, invalid
	}
	if priority == s.Priority && table == s.Table && row[7] == unix.FR_ACT_TO_TBL {
		return 2, nil
	}
	if priority == s.Priority+1 && table == 0 && row[7] == unix.FR_ACT_PROHIBIT {
		return 4, nil
	}
	return 0, invalid
}

// ZapretTUNForward is the bounded rule input, with no link or FD authority.
type ZapretTUNForward struct {
	Scope          ZapretTUNScope
	IngressIfIndex int
	DeviceIfIndex  int
}

func (p ZapretTUNForward) Valid() bool {
	return p.Scope.valid() && p.IngressIfIndex > 0 && p.DeviceIfIndex > 0 && p.IngressIfIndex != p.DeviceIfIndex
}
