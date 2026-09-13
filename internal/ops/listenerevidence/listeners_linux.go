//go:build linux

// Package listenerevidence owns bounded process-generation and socket-table
// observation. It returns socket identity only; callers retain supervisor,
// executable, configuration, and semantic ownership authority.
package listenerevidence

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

const (
	maxSocketTableBytes   = int64(4 << 20)
	maxObservationRetries = 3
)

// Reason is a stable, non-sensitive failure class suitable for privileged
// diagnostics. It deliberately carries no path, command line, or raw error.
type Reason string

const (
	ReasonInvalidProcess      Reason = "invalid_process"
	ReasonPIDFDAuthority      Reason = "pidfd_authority_unavailable"
	ReasonDescriptorInventory Reason = "descriptor_inventory_unavailable"
	ReasonDescriptorBound     Reason = "descriptor_inventory_bound_exceeded"
	ReasonSocketTable         Reason = "socket_table_unavailable"
	ReasonProcessChanged      Reason = "process_generation_changed"
	ReasonObservationChanged  Reason = "socket_observation_changed"
	ReasonSocketAmbiguous     Reason = "socket_identity_ambiguous"
)

// Diagnostic describes where listener evidence failed or which proof path
// succeeded. ErrnoClass is a bounded symbolic class, never raw errno text.
type Diagnostic struct {
	ProofMethod           string `json:"proofMethod,omitempty"`
	Stage                 string `json:"stage"`
	ErrnoClass            string `json:"errnoClass,omitempty"`
	DescriptorCount       int    `json:"descriptorCount"`
	SocketDescriptorCount int    `json:"socketDescriptorCount"`
	DuplicateAttempts     int    `json:"duplicateAttempts"`
	DuplicatedSockets     int    `json:"duplicatedSockets"`
	FallbackUsed          bool   `json:"fallbackUsed"`
	RetryCount            int    `json:"retryCount"`
}

// Observation is the exact result and its bounded diagnostic provenance.
type Observation struct {
	Sockets    []hostfacts.ListenerSocketIdentityV1 `json:"sockets"`
	Diagnostic Diagnostic                           `json:"diagnostic"`
}

type listenerEnvironment struct {
	pidfdOpen               func(int, int) (int, error)
	closeFD                 func(int) error
	readProcessStart        func(int) (string, error)
	readDescriptorSnapshot  func(int) (processevidence.DescriptorSnapshot, error)
	readSocketTables        func(int, hostfacts.Network, map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error)
	pidfdGetfd              func(int, int, int) (int, error)
	inspectDuplicatedSocket func(int, hostfacts.Network, map[uint16]bool) (hostfacts.ListenerSocketIdentityV1, bool)
	pidfdAlive              func(int) bool
}

func productionListenerEnvironment() listenerEnvironment {
	return listenerEnvironment{
		pidfdOpen: unix.PidfdOpen, closeFD: unix.Close, readProcessStart: readProcessStart,
		readDescriptorSnapshot: readDescriptorSnapshot, readSocketTables: readSocketTables,
		pidfdGetfd: unix.PidfdGetfd, inspectDuplicatedSocket: inspectDuplicatedSocket, pidfdAlive: pidfdAlive,
	}
}

// Error is intentionally safe to retain in privileged evidence. Error()
// exposes only the stable class and stage.
type Error struct {
	Reason     Reason
	Diagnostic Diagnostic
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Diagnostic.Stage == "" {
		return string(e.Reason)
	}
	return string(e.Reason) + ":" + e.Diagnostic.Stage
}

// BrokerDiagnostic is a structural seam consumed by the privileged broker.
// Keeping primitive return values avoids importing the broker into this
// low-level package while preserving the exact bounded failure branch.
func (e *Error) BrokerDiagnostic() (owner, reason, stage, errno, proofMethod string) {
	if e == nil {
		return "", "", "", "", ""
	}
	return "listener_evidence", string(e.Reason), e.Diagnostic.Stage, e.Diagnostic.ErrnoClass, e.Diagnostic.ProofMethod
}

// BrokerDiagnosticCounters exposes only the fixed bounded counters already
// classified by the listener owner. The broker neither interprets nor derives
// these values.
func (e *Error) BrokerDiagnosticCounters() (descriptorCount, socketDescriptorCount, duplicateAttempts, duplicatedSockets, retryCount int) {
	if e == nil {
		return 0, 0, 0, 0, 0
	}
	return e.Diagnostic.DescriptorCount, e.Diagnostic.SocketDescriptorCount,
		e.Diagnostic.DuplicateAttempts, e.Diagnostic.DuplicatedSockets, e.Diagnostic.RetryCount
}

// DiagnosticOf extracts safe listener diagnostics without exposing an
// underlying host error.
func DiagnosticOf(err error) (Reason, Diagnostic, bool) {
	var typed *Error
	if !errors.As(err, &typed) {
		return "", Diagnostic{}, false
	}
	return typed.Reason, typed.Diagnostic, true
}

// ObserveAcceptingTCP returns accepting TCP sockets owned by pid. It keeps the
// historical API while the detailed form exposes proof provenance.
func ObserveAcceptingTCP(ctx context.Context, pid int, allowedPorts map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
	observation, err := ObserveAcceptingTCPDetailed(ctx, pid, allowedPorts)
	return observation.Sockets, err
}

func ObserveAcceptingTCPDetailed(ctx context.Context, pid int, allowedPorts map[uint16]bool) (Observation, error) {
	return ObserveProcessSockets(ctx, pid, hostfacts.NetworkTCP, allowedPorts)
}

// ObserveProcessSockets establishes a pidfd liveness fence, then binds a
// stable /proc/<pid>/fd socket-inode set to stable socket-table rows from the
// same network namespace. pidfd_getfd is attempted only as an enrichment; its
// denial never invalidates the procfs proof.
func ObserveProcessSockets(ctx context.Context, pid int, network hostfacts.Network, allowedPorts map[uint16]bool) (Observation, error) {
	return observeProcessSocketsWith(ctx, pid, network, allowedPorts, productionListenerEnvironment())
}

func observeProcessSocketsWith(ctx context.Context, pid int, network hostfacts.Network, allowedPorts map[uint16]bool, environment listenerEnvironment) (Observation, error) {
	diagnostic := Diagnostic{Stage: "validate_request"}
	if pid <= 1 || network != hostfacts.NetworkTCP && network != hostfacts.NetworkUDP {
		return Observation{}, listenerError(ReasonInvalidProcess, diagnostic)
	}
	diagnostic.ProofMethod = hostfacts.ListenerProofProcFSV1
	pidfd, err := environment.pidfdOpen(pid, 0)
	if err != nil {
		diagnostic.Stage, diagnostic.ErrnoClass = "pidfd_open", errnoClass(err)
		return Observation{}, listenerError(ReasonPIDFDAuthority, diagnostic)
	}
	defer environment.closeFD(pidfd)

	var last error
	for attempt := 0; attempt < maxObservationRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		observation, err := observeOnce(ctx, pidfd, pid, network, allowedPorts, environment)
		observation.Diagnostic.RetryCount = attempt
		if err == nil {
			return observation, nil
		}
		setDiagnosticRetry(err, attempt)
		last = err
		reason, _, ok := DiagnosticOf(err)
		if !ok || reason != ReasonObservationChanged && reason != ReasonProcessChanged {
			return Observation{}, err
		}
	}
	return Observation{}, last
}

func observeOnce(ctx context.Context, pidfd, pid int, network hostfacts.Network, allowedPorts map[uint16]bool, environment listenerEnvironment) (Observation, error) {
	diagnostic := Diagnostic{Stage: "process_generation_before", ProofMethod: hostfacts.ListenerProofProcFSV1}
	startBefore, err := environment.readProcessStart(pid)
	if err != nil {
		diagnostic.ErrnoClass = errnoClass(err)
		return Observation{}, listenerError(ReasonProcessChanged, diagnostic)
	}
	before, err := environment.readDescriptorSnapshot(pid)
	if err != nil {
		return Observation{}, err
	}
	diagnostic.DescriptorCount = before.Count
	diagnostic.SocketDescriptorCount = len(before.SocketInodes)
	rowsBefore, err := environment.readSocketTables(pid, network, allowedPorts)
	if err != nil {
		return Observation{}, mergeDiagnosticContext(err, diagnostic)
	}

	strong := make(map[string]hostfacts.ListenerSocketIdentityV1)
	fdNumbers := make([]int, 0, len(before.SocketInodes))
	for fd := range before.SocketInodes {
		fdNumbers = append(fdNumbers, fd)
	}
	sort.Ints(fdNumbers)
	for _, targetFD := range fdNumbers {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		diagnostic.DuplicateAttempts++
		fd, duplicateErr := environment.pidfdGetfd(pidfd, targetFD, 0)
		if duplicateErr != nil {
			if diagnostic.ErrnoClass == "" {
				diagnostic.ErrnoClass = errnoClass(duplicateErr)
			}
			continue
		}
		socket, ok := environment.inspectDuplicatedSocket(fd, network, allowedPorts)
		_ = environment.closeFD(fd)
		if !ok || socket.Inode != before.SocketInodes[targetFD] {
			continue
		}
		diagnostic.DuplicatedSockets++
		strong[socketKey(socket)] = socket
	}

	rowsAfter, err := environment.readSocketTables(pid, network, allowedPorts)
	if err != nil {
		return Observation{}, mergeDiagnosticContext(err, diagnostic)
	}
	after, err := environment.readDescriptorSnapshot(pid)
	if err != nil {
		return Observation{}, mergeDiagnosticContext(err, diagnostic)
	}
	startAfter, startErr := environment.readProcessStart(pid)
	if startErr != nil || startBefore != startAfter || !environment.pidfdAlive(pidfd) {
		diagnostic.Stage, diagnostic.ErrnoClass = "process_generation_after", errnoClass(startErr)
		return Observation{}, listenerError(ReasonProcessChanged, diagnostic)
	}
	if !sameDescriptorSockets(before.SocketInodes, after.SocketInodes) || !sameSocketRows(rowsBefore, rowsAfter) {
		diagnostic.Stage = "stability_fence"
		return Observation{}, listenerError(ReasonObservationChanged, diagnostic)
	}

	result := make([]hostfacts.ListenerSocketIdentityV1, 0)
	seenRows := make(map[string]bool)
	for _, row := range rowsAfter {
		if !descriptorOwnsInode(after.SocketInodes, row.Inode) {
			continue
		}
		key := socketKey(row)
		if seenRows[key] {
			continue
		}
		seenRows[key] = true
		if enriched, ok := strong[key]; ok {
			result = append(result, enriched)
			continue
		}
		result = append(result, row)
	}
	sortSockets(result)
	diagnostic.Stage = "complete"
	diagnostic.FallbackUsed = diagnostic.DuplicatedSockets < len(result)
	if len(result) > 0 && diagnostic.DuplicatedSockets >= len(result) {
		diagnostic.ProofMethod = hostfacts.ListenerProofPIDFDSocketV1
	} else {
		diagnostic.ProofMethod = hostfacts.ListenerProofProcFSV1
	}
	return Observation{Sockets: result, Diagnostic: diagnostic}, nil
}

func readDescriptorSnapshot(pid int) (processevidence.DescriptorSnapshot, error) {
	diagnostic := Diagnostic{Stage: "descriptor_inventory", ProofMethod: hostfacts.ListenerProofProcFSV1}
	snapshot, err := processevidence.ObserveDescriptorSnapshot(pid)
	if err != nil {
		diagnostic.ErrnoClass = errnoClass(err)
		if errors.Is(err, processevidence.ErrDescriptorInventoryBound) {
			return processevidence.DescriptorSnapshot{}, listenerError(ReasonDescriptorBound, diagnostic)
		}
		return processevidence.DescriptorSnapshot{}, listenerError(ReasonDescriptorInventory, diagnostic)
	}
	return snapshot, nil
}

func socketLinkInode(target string) (string, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return "", false
	}
	inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
	if inode == "" {
		return "", false
	}
	for _, r := range inode {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return inode, true
}

func readSocketTables(pid int, network hostfacts.Network, allowedPorts map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
	result := make([]hostfacts.ListenerSocketIdentityV1, 0)
	opened := 0
	for _, family := range []hostfacts.Family{hostfacts.FamilyIPv4, hostfacts.FamilyIPv6} {
		suffix := string(network)
		if family == hostfacts.FamilyIPv6 {
			suffix += "6"
		}
		path := filepath.Join("/proc", strconv.Itoa(pid), "net", suffix)
		file, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			diagnostic := Diagnostic{Stage: "socket_table_open", ErrnoClass: errnoClass(err), ProofMethod: hostfacts.ListenerProofProcFSV1}
			return nil, listenerError(ReasonSocketTable, diagnostic)
		}
		opened++
		data, readErr := io.ReadAll(io.LimitReader(file, maxSocketTableBytes+1))
		_ = file.Close()
		if readErr != nil || int64(len(data)) > maxSocketTableBytes {
			diagnostic := Diagnostic{Stage: "socket_table_read", ErrnoClass: errnoClass(readErr), ProofMethod: hostfacts.ListenerProofProcFSV1}
			return nil, listenerError(ReasonSocketTable, diagnostic)
		}
		rows, parseErr := parseSocketTable(data, network, family, allowedPorts)
		if parseErr != nil {
			return nil, parseErr
		}
		result = append(result, rows...)
	}
	if opened == 0 {
		return nil, listenerError(ReasonSocketTable, Diagnostic{Stage: "socket_table_missing", ProofMethod: hostfacts.ListenerProofProcFSV1})
	}
	sortSockets(result)
	return result, nil
}

func parseSocketTable(data []byte, network hostfacts.Network, family hostfacts.Family, allowedPorts map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
	result := make([]hostfacts.ListenerSocketIdentityV1, 0)
	byInode := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || network == hostfacts.NetworkTCP && fields[3] != "0A" || network == hostfacts.NetworkUDP && !remoteUnspecified(fields[2]) {
			continue
		}
		address, port, ok := parseProcAddress(fields[1], family)
		if !ok || port == 0 || len(allowedPorts) != 0 && !allowedPorts[port] {
			continue
		}
		inode := fields[9]
		if _, ok := socketLinkInode("socket:[" + inode + "]"); !ok {
			continue
		}
		row := hostfacts.ListenerSocketIdentityV1{
			ProofMethod: hostfacts.ListenerProofProcFSV1, Network: network, Family: family, Bind: address.String(), Port: port,
			Inode: inode, Wildcard: address.IsUnspecified(), CoverageFamilies: []hostfacts.Family{family},
		}
		key := socketKey(row)
		if prior, exists := byInode[inode]; exists && prior != key {
			return nil, listenerError(ReasonSocketAmbiguous, Diagnostic{Stage: "socket_table_parse", ProofMethod: hostfacts.ListenerProofProcFSV1})
		}
		byInode[inode] = key
		result = append(result, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, listenerError(ReasonSocketTable, Diagnostic{Stage: "socket_table_parse", ErrnoClass: errnoClass(err), ProofMethod: hostfacts.ListenerProofProcFSV1})
	}
	return result, nil
}

func remoteUnspecified(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || strings.Trim(parts[0], "0") != "" {
		return false
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	return err == nil && port == 0
}

func parseProcAddress(value string, family hostfacts.Family) (netip.Addr, uint16, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil || family == hostfacts.FamilyIPv4 && len(raw) != 4 || family == hostfacts.FamilyIPv6 && len(raw) != 16 {
		return netip.Addr{}, 0, false
	}
	for index := 0; index < len(raw); index += 4 {
		raw[index], raw[index+3] = raw[index+3], raw[index]
		raw[index+1], raw[index+2] = raw[index+2], raw[index+1]
	}
	address, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.Addr{}, 0, false
	}
	return address, uint16(port), true
}

func inspectDuplicatedSocket(fd int, network hostfacts.Network, allowedPorts map[uint16]bool) (hostfacts.ListenerSocketIdentityV1, bool) {
	stat := unix.Stat_t{}
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino == 0 {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	typeValue, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || network == hostfacts.NetworkTCP && typeValue != unix.SOCK_STREAM || network == hostfacts.NetworkUDP && typeValue != unix.SOCK_DGRAM {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	if network == hostfacts.NetworkTCP {
		accepting, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
		if err != nil || accepting != 1 {
			return hostfacts.ListenerSocketIdentityV1{}, false
		}
	}
	sockaddr, err := unix.Getsockname(fd)
	if err != nil {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	var family hostfacts.Family
	var address netip.Addr
	var port int
	var ipv6Only *bool
	switch value := sockaddr.(type) {
	case *unix.SockaddrInet4:
		family, address, port = hostfacts.FamilyIPv4, netip.AddrFrom4(value.Addr), value.Port
	case *unix.SockaddrInet6:
		family, address, port = hostfacts.FamilyIPv6, netip.AddrFrom16(value.Addr), value.Port
		flag, flagErr := unix.GetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY)
		if flagErr != nil || flag != 0 && flag != 1 {
			return hostfacts.ListenerSocketIdentityV1{}, false
		}
		only := flag == 1
		ipv6Only = &only
	default:
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	if port <= 0 || port > 65535 || len(allowedPorts) != 0 && !allowedPorts[uint16(port)] {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil || cookie == 0 {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	coverage := []hostfacts.Family{family}
	if family == hostfacts.FamilyIPv6 && address.IsUnspecified() && ipv6Only != nil && !*ipv6Only {
		coverage = []hostfacts.Family{hostfacts.FamilyIPv4, hostfacts.FamilyIPv6}
	}
	return hostfacts.ListenerSocketIdentityV1{
		ProofMethod: hostfacts.ListenerProofPIDFDSocketV1, Network: network, Family: family, Bind: address.String(), Port: uint16(port),
		Inode: strconv.FormatUint(stat.Ino, 10), Cookie: cookie, Wildcard: address.IsUnspecified(), IPv6Only: ipv6Only,
		CoverageFamilies: coverage,
	}, true
}

func readProcessStart(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", err
	}
	close := strings.LastIndex(string(data), ") ")
	if close < 0 {
		return "", errors.New("malformed process stat")
	}
	fields := strings.Fields(string(data)[close+2:])
	if len(fields) <= 19 {
		return "", errors.New("short process stat")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New("invalid process start")
	}
	return fields[19], nil
}

func pidfdAlive(pidfd int) bool {
	poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	count, err := unix.Poll(poll, 0)
	return err == nil && count == 0 && poll[0].Revents == 0
}

func descriptorOwnsInode(values map[int]string, inode string) bool {
	for _, value := range values {
		if value == inode {
			return true
		}
	}
	return false
}

func sameDescriptorSockets(left, right map[int]string) bool {
	if len(left) != len(right) {
		return false
	}
	for fd, inode := range left {
		if right[fd] != inode {
			return false
		}
	}
	return true
}

func sameSocketRows(left, right []hostfacts.ListenerSocketIdentityV1) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if socketKey(left[index]) != socketKey(right[index]) {
			return false
		}
	}
	return true
}

func socketKey(value hostfacts.ListenerSocketIdentityV1) string {
	return string(value.Network) + "\x00" + string(value.Family) + "\x00" + value.Bind + "\x00" + strconv.Itoa(int(value.Port)) + "\x00" + value.Inode
}

func sortSockets(values []hostfacts.ListenerSocketIdentityV1) {
	sort.Slice(values, func(i, j int) bool { return socketKey(values[i]) < socketKey(values[j]) })
}

func listenerError(reason Reason, diagnostic Diagnostic) error {
	return &Error{Reason: reason, Diagnostic: diagnostic}
}

func mergeDiagnosticContext(err error, context Diagnostic) error {
	var typed *Error
	if !errors.As(err, &typed) {
		return err
	}
	if typed.Diagnostic.DescriptorCount == 0 {
		typed.Diagnostic.DescriptorCount = context.DescriptorCount
	}
	if typed.Diagnostic.SocketDescriptorCount == 0 {
		typed.Diagnostic.SocketDescriptorCount = context.SocketDescriptorCount
	}
	if typed.Diagnostic.DuplicateAttempts == 0 {
		typed.Diagnostic.DuplicateAttempts = context.DuplicateAttempts
	}
	if typed.Diagnostic.DuplicatedSockets == 0 {
		typed.Diagnostic.DuplicatedSockets = context.DuplicatedSockets
	}
	return err
}

func setDiagnosticRetry(err error, retry int) {
	var typed *Error
	if errors.As(err, &typed) {
		typed.Diagnostic.RetryCount = retry
	}
}

func errnoClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, unix.EPERM):
		return "EPERM"
	case errors.Is(err, unix.EACCES):
		return "EACCES"
	case errors.Is(err, unix.ENOSYS):
		return "ENOSYS"
	case errors.Is(err, unix.EINVAL):
		return "EINVAL"
	case errors.Is(err, unix.EBADF):
		return "EBADF"
	case errors.Is(err, unix.ESRCH):
		return "ESRCH"
	case errors.Is(err, unix.ENOENT):
		return "ENOENT"
	case errors.Is(err, unix.EIO):
		return "EIO"
	default:
		return "OTHER"
	}
}
