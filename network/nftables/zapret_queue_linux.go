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

const (
	zapretQueueInventoryLimit = 1 << 20
	zapretEngineSocketLimit   = 64
)

// ZapretQueueOwner is a fresh observation, not a grant or a durable receipt.
// A port ID is a netlink address, not a Linux PID.
type ZapretQueueOwner struct {
	Queue           uint16
	PortID          uint32
	SocketInode     uint64
	ProcessRevision string
}

type zapretEngineSockets struct {
	queue ZapretQueueOwner
	raw   [2]uint64 // IPv4 and IPv6 SO_COOKIE values (zero means absent).
}

// ObserveZapretQueueOwner proves the selected queue belongs to the engine
// the privileged launcher already authenticated. Never derive expected from
// a queue port ID, on-disk PID file, or a Panel request.
func ObserveZapretQueueOwner(plan planner.ZapretPlan, expected processevidence.Fact) (ZapretQueueOwner, error) {
	sockets, err := observeZapretEngineSockets(plan, expected)
	return sockets.queue, err
}

func observeZapretEngineSockets(plan planner.ZapretPlan, expected processevidence.Fact) (zapretEngineSockets, error) {
	scope, err := plan.SocketScope()
	if err != nil || expected.PID <= 1 || !validSHA256(expected.Revision) || !validSHA256(expected.ExeDigest) ||
		expected.ExecutableDeleted || expected.ExeUID != 0 || expected.ExeMode&0o022 != 0 {
		return zapretEngineSockets{}, errors.New("queue for Zapret has no trusted engine identity")
	}
	pidfd, err := unix.PidfdOpen(expected.PID, 0)
	if err != nil {
		return zapretEngineSockets{}, fmt.Errorf("open Zapret engine pidfd: %w", err)
	}
	defer unix.Close(pidfd)
	process, err := processevidence.Observe(expected.PID)
	if err != nil || process.Revision != expected.Revision || !zapretPidfdAlive(pidfd) {
		return zapretEngineSockets{}, errors.New("process generation for the Zapret engine changed")
	}
	port, inode, err := observeZapretQueueBinding(plan.Queue())
	if err != nil {
		return zapretEngineSockets{}, err
	}
	descriptors, err := processevidence.ObserveDescriptorSnapshot(expected.PID)
	if err != nil {
		return zapretEngineSockets{}, err
	}
	if len(descriptors.SocketInodes) > zapretEngineSocketLimit {
		return zapretEngineSockets{}, errors.New("socket inventory for the Zapret engine exceeds its bound")
	}
	var result zapretEngineSockets
	egress := scope.Egress()
	for fd, inodeText := range descriptors.SocketInodes {
		observed, err := strconv.ParseUint(inodeText, 10, 64)
		if err != nil {
			return zapretEngineSockets{}, err
		}
		dup, err := unix.PidfdGetfd(pidfd, fd, 0)
		if err != nil {
			return zapretEngineSockets{}, fmt.Errorf("duplicate Zapret engine socket for identity proof: %w", err)
		}
		err = result.observeSocket(dup, observed, inode, egress.NamespaceCookie, port)
		_ = unix.Close(dup)
		if err != nil {
			return zapretEngineSockets{}, err
		}
	}
	if result.queue.PortID == 0 || egress.IPv4Gateway.IsValid() && result.raw[0] == 0 || egress.IPv6Gateway.IsValid() && result.raw[1] == 0 {
		return zapretEngineSockets{}, errors.New("authenticated engine for Zapret does not own the queue and required raw sockets")
	}
	afterPort, afterInode, err := observeZapretQueueBinding(plan.Queue())
	if err != nil || afterPort != port || afterInode != inode || !zapretPidfdAlive(pidfd) {
		return zapretEngineSockets{}, errors.New("queue binding for Zapret changed during observation")
	}
	afterProcess, err := processevidence.Observe(expected.PID)
	if err != nil || afterProcess.Revision != process.Revision || !zapretPidfdAlive(pidfd) {
		return zapretEngineSockets{}, errors.New("process for the Zapret engine changed during socket observation")
	}
	result.queue.Queue, result.queue.ProcessRevision = plan.Queue(), process.Revision
	return result, nil
}

func zapretPidfdAlive(fd int) bool {
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0)
	return n == 0 && err == nil
}

func (s *zapretEngineSockets) observeSocket(fd int, inode, queueInode, namespace uint64, port uint32) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino != inode {
		return errors.New("descriptor for the Zapret engine changed during socket observation")
	}
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	protocol, protocolErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil || protocolErr != nil {
		return errors.New("socket kind for the Zapret engine is unavailable")
	}
	if inode != queueInode && ((domain != unix.AF_INET && domain != unix.AF_INET6) || protocol != unix.IPPROTO_RAW) {
		return nil
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil || cookie != namespace {
		return errors.New("socket for the Zapret engine is outside the planned network namespace")
	}
	if inode == queueInode {
		address, err := unix.Getsockname(fd)
		netlink, ok := address.(*unix.SockaddrNetlink)
		if err != nil || !ok || domain != unix.AF_NETLINK || protocol != unix.NETLINK_NETFILTER || netlink.Pid != port || s.queue.PortID != 0 {
			return errors.New("netlink descriptor for the Zapret engine is ambiguous or does not match the queue")
		}
		s.queue.PortID, s.queue.SocketInode = port, inode
		return nil
	}
	family := 0
	if domain == unix.AF_INET6 {
		family = 1
	}
	kind, kindErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	rawCookie, cookieErr := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if kindErr != nil || kind != unix.SOCK_RAW || cookieErr != nil || rawCookie == 0 || s.raw[family] != 0 {
		return errors.New("raw socket for the Zapret engine is unavailable or ambiguous")
	}
	s.raw[family] = rawCookie
	return nil
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
		return "", fmt.Errorf("read queue inventory for Zapret: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, zapretQueueInventoryLimit+1))
	if err != nil || len(data) > zapretQueueInventoryLimit {
		return "", errors.New("queue inventory for Zapret exceeded its bound or could not be read")
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
			return 0, errors.New("queue owner or packet copy mode for Zapret is ambiguous")
		}
		value, err := strconv.ParseUint(fields[1], 10, 32)
		copyRange, rangeErr := strconv.ParseUint(fields[4], 10, 32)
		if err != nil || value == 0 || rangeErr != nil || copyRange == 0 {
			return 0, errors.New("queue for Zapret has no valid packet consumer")
		}
		port = uint32(value)
	}
	if port == 0 {
		return 0, errors.New("queue for Zapret is not bound")
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
			return 0, errors.New("netlink socket binding for Zapret is ambiguous")
		}
		value, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil || value == 0 {
			return 0, errors.New("netlink socket inode for Zapret is invalid")
		}
		inode = value
	}
	if inode == 0 {
		return 0, errors.New("netlink socket binding for Zapret is missing")
	}
	return inode, nil
}
