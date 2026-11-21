//go:build linux && !minimal

package nftables

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

const zapretQueueInventoryLimit = 1 << 20

// ZapretQueueOwner is a fresh observation, not a grant or a durable receipt.
// A port ID is a netlink address, not a Linux PID.
type ZapretQueueOwner struct {
	Queue           uint16
	PortID          uint32
	SocketInode     uint64
	ProcessRevision string
}

// ObserveZapretQueueOwner proves the selected queue belongs to the engine
// the privileged launcher already authenticated. Never derive expected from
// a queue port ID, on-disk PID file, or a Panel request.
func ObserveZapretQueueOwner(plan planner.ZapretPlan, expected processevidence.Fact) (ZapretQueueOwner, error) {
	scope, err := plan.SocketScope()
	if err != nil || expected.PID <= 1 || !validSHA256(expected.Revision) || !validSHA256(expected.ExeDigest) ||
		expected.ExecutableDeleted || expected.ExeUID != 0 || expected.ExeMode&0o022 != 0 {
		return ZapretQueueOwner{}, errors.New("Zapret queue has no trusted engine identity")
	}
	pidfd, err := unix.PidfdOpen(expected.PID, 0)
	if err != nil {
		return ZapretQueueOwner{}, fmt.Errorf("open Zapret engine pidfd: %w", err)
	}
	defer unix.Close(pidfd)
	process, err := processevidence.Observe(expected.PID)
	if err != nil || process.Revision != expected.Revision || !zapretPidfdAlive(pidfd) {
		return ZapretQueueOwner{}, errors.New("Zapret engine process generation changed")
	}
	port, inode, err := observeZapretQueueBinding(plan.Queue())
	if err != nil {
		return ZapretQueueOwner{}, err
	}
	descriptors, err := processevidence.ObserveDescriptorSnapshot(expected.PID)
	if err != nil {
		return ZapretQueueOwner{}, err
	}
	wantInode := strconv.FormatUint(inode, 10)
	for fd, observed := range descriptors.SocketInodes {
		if observed != wantInode {
			continue
		}
		matched, err := verifyZapretQueueSocket(pidfd, fd, port, inode, scope.Egress().NamespaceCookie)
		if err != nil {
			return ZapretQueueOwner{}, err
		}
		if !matched {
			continue
		}
		afterPort, afterInode, err := observeZapretQueueBinding(plan.Queue())
		if err != nil || afterPort != port || afterInode != inode || !zapretPidfdAlive(pidfd) {
			return ZapretQueueOwner{}, errors.New("Zapret queue binding changed during observation")
		}
		afterProcess, err := processevidence.Observe(expected.PID)
		if err != nil || afterProcess.Revision != process.Revision || !zapretPidfdAlive(pidfd) {
			return ZapretQueueOwner{}, errors.New("Zapret engine process changed during queue observation")
		}
		return ZapretQueueOwner{plan.Queue(), port, inode, process.Revision}, nil
	}
	return ZapretQueueOwner{}, errors.New("Zapret queue does not belong to the authenticated engine")
}

func zapretPidfdAlive(fd int) bool {
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0)
	return n == 0 && err == nil
}

func verifyZapretQueueSocket(pidfd, fd int, port uint32, inode, namespace uint64) (bool, error) {
	dup, err := unix.PidfdGetfd(pidfd, fd, 0)
	if err != nil {
		return false, fmt.Errorf("duplicate Zapret engine netlink descriptor for identity proof: %w", err)
	}
	defer unix.Close(dup)
	var stat unix.Stat_t
	if err := unix.Fstat(dup, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino != inode {
		return false, nil
	}
	protocol, err := unix.GetsockoptInt(dup, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil || protocol != unix.NETLINK_NETFILTER {
		return false, nil
	}
	cookie, err := unix.GetsockoptUint64(dup, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil || cookie != namespace {
		return false, nil
	}
	address, err := unix.Getsockname(dup)
	netlink, ok := address.(*unix.SockaddrNetlink)
	return err == nil && ok && netlink.Pid == port, nil
}

func observeZapretQueueBinding(queue uint16) (uint32, uint64, error) {
	queues, err := readZapretQueueInventory("/proc/net/netfilter/nfnetlink_queue")
	if err != nil {
		return 0, 0, err
	}
	port, err := zapretQueuePort(queues, queue)
	if err != nil {
		return 0, 0, err
	}
	netlink, err := readZapretQueueInventory("/proc/net/netlink")
	if err != nil {
		return 0, 0, err
	}
	inode, err := zapretQueueSocketInode(netlink, port)
	return port, inode, err
}

func readZapretQueueInventory(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Zapret queue inventory unavailable: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, zapretQueueInventoryLimit+1))
	if err != nil || len(data) > zapretQueueInventoryLimit {
		return "", errors.New("Zapret queue inventory exceeded its bound or could not be read")
	}
	return string(data), nil
}

func zapretQueuePort(inventory string, queue uint16) (uint32, error) {
	want := strconv.Itoa(int(queue))
	var port uint32
	for row := range strings.SplitSeq(inventory, "\n") {
		fields := strings.Fields(row)
		if len(fields) == 0 || fields[0] != want {
			continue
		}
		if len(fields) != 9 || fields[3] != "2" || port != 0 {
			return 0, errors.New("Zapret queue owner or packet copy mode is ambiguous")
		}
		value, err := strconv.ParseUint(fields[1], 10, 32)
		copyRange, rangeErr := strconv.ParseUint(fields[4], 10, 32)
		if err != nil || value == 0 || rangeErr != nil || copyRange == 0 {
			return 0, errors.New("Zapret queue has no valid packet consumer")
		}
		port = uint32(value)
	}
	if port == 0 {
		return 0, errors.New("Zapret queue is not bound")
	}
	return port, nil
}

func zapretQueueSocketInode(inventory string, port uint32) (uint64, error) {
	protocol := strconv.Itoa(unix.NETLINK_NETFILTER)
	want := strconv.FormatUint(uint64(port), 10)
	var inode uint64
	for row := range strings.SplitSeq(inventory, "\n") {
		fields := strings.Fields(row)
		if len(fields) < 3 || fields[1] != protocol || fields[2] != want {
			continue
		}
		if len(fields) != 10 || inode != 0 {
			return 0, errors.New("Zapret netlink socket binding is ambiguous")
		}
		value, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil || value == 0 {
			return 0, errors.New("Zapret netlink socket inode is invalid")
		}
		inode = value
	}
	if inode == 0 {
		return 0, errors.New("Zapret netlink socket binding is missing")
	}
	return inode, nil
}
