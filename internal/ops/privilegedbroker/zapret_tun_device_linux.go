//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	route "github.com/sagernet/netlink"
	"golang.org/x/sys/unix"
)

// This broker-owned handle is private to one generation. The Planner
// validates the scope, but never opens or mutates the TUN device itself.
type preparedZapretTUN struct {
	file    *os.File
	link    route.Link
	route   route.Route
	alias   string
	forward planner.ZapretTUNForward
}

// Preflight checks access without creating a device or opening it to Panel.
func checkZapretTUNDevice() error {
	fd, err := openZapretTUNDevice()
	if err != nil {
		return err
	}
	features, err := unix.IoctlGetUint32(fd, unix.TUNGETFEATURES)
	if err == nil && features&(unix.IFF_TUN|unix.IFF_NO_PI) != unix.IFF_TUN|unix.IFF_NO_PI {
		err = errors.New("TUN layer-3 device without packet headers is unavailable")
	}
	return errors.Join(err, unix.Close(fd))
}

func openZapretTUNDevice() (int, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open broker TUN device: %w", err)
	}
	return fd, nil
}

func prepareZapretTUN(ctx context.Context, s planner.ZapretTUNScope, egress int, mtu uint32, generation string) (_ *preparedZapretTUN, failure error) {
	ingress, err := planner.CheckZapretTUNScope(ctx, s, egress)
	if err != nil {
		return nil, err
	}
	if mtu < 1280 || mtu > 1500 || !digestPattern.MatchString(generation) {
		return nil, errors.New("invalid installed Zapret TUN device parameters")
	}
	h, err := zapretTUNRouteHandle()
	if err != nil {
		return nil, err
	}
	defer h.Close()
	table := route.Route{Table: int(s.Table)}
	if rows, err := h.RouteListFiltered(route.FAMILY_V4, &table, route.RT_FILTER_TABLE); err != nil || len(rows) != 0 {
		return nil, errors.Join(errors.New("reserved Zapret TUN route table is not empty"), err)
	}
	fd, err := openZapretTUNDevice()
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "prepared-zapret-tun")
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, file.Close())
		}
	}()
	request, err := unix.NewIfreq(planner.ZapretTUNName)
	if err != nil {
		return nil, err
	}
	request.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI | unix.IFF_TUN_EXCL)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, request); err != nil {
		return nil, fmt.Errorf("create exclusive Zapret TUN device: %w", err)
	}
	link, err := h.LinkByName(planner.ZapretTUNName)
	if err != nil {
		return nil, err
	}
	alias := "solovey-zapret:" + generation
	if err := h.LinkSetAlias(link, alias); err != nil {
		return nil, err
	}
	if err := h.LinkSetMTU(link, int(mtu)); err != nil {
		return nil, err
	}
	forward := planner.ZapretTUNForward{Scope: s, IngressIfIndex: ingress, DeviceIfIndex: link.Attrs().Index}
	table.LinkIndex, table.Scope, table.Protocol = forward.DeviceIfIndex, route.SCOPE_LINK, unix.RTPROT_STATIC
	table.Dst = &net.IPNet{IP: s.Destination.Addr().AsSlice(), Mask: net.CIDRMask(s.Destination.Bits(), 32)}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &preparedZapretTUN{file: file, link: link, route: table, alias: alias, forward: forward}, nil
}

func zapretTUNRouteHandle() (*route.Handle, error) {
	h, err := route.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	if err := h.SetSocketTimeout(time.Second); err != nil {
		h.Close()
		return nil, err
	}
	return h, nil
}

func (t *preparedZapretTUN) forwardPlan() *planner.ZapretTUNForward {
	if t == nil {
		return nil
	}
	return &t.forward
}

// Activate only after both guards exist. Do not replace another owner's route.
func (t *preparedZapretTUN) activate() error {
	h, err := zapretTUNRouteHandle()
	if err != nil {
		return err
	}
	defer h.Close()
	if err := h.LinkSetUp(t.link); err != nil {
		return err
	}
	return h.RouteAdd(&t.route)
}

// Revoke the link, not merely the broker's copy of the FD. The retained
// guard is required when broker death prevents this method from running.
func (t *preparedZapretTUN) close() error {
	h, err := zapretTUNRouteHandle()
	if err != nil {
		return errors.Join(err, t.file.Close())
	}
	defer h.Close()
	live, err := h.LinkByIndex(t.forward.DeviceIfIndex)
	var missing route.LinkNotFoundError
	if err == nil && (live.Attrs().Name != planner.ZapretTUNName || live.Attrs().Alias != t.alias) {
		err = errors.New("refusing to retire a replaced Zapret TUN link")
	} else if err == nil {
		err = h.LinkDel(live)
	} else if errors.As(err, &missing) {
		err = nil
	}
	return errors.Join(err, t.file.Close())
}
