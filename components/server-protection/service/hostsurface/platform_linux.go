//go:build linux

package hostsurface

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

func observePlatform(ctx context.Context, limits hostfacts.Limits) (PlatformSnapshot, error) {
	result := PlatformSnapshot{}
	remaining := limits.MaxDecodedBytes
	for _, spec := range []struct {
		path    string
		network hostfacts.Network
		family  hostfacts.Family
	}{
		{"/proc/net/tcp", hostfacts.NetworkTCP, hostfacts.FamilyIPv4}, {"/proc/net/tcp6", hostfacts.NetworkTCP, hostfacts.FamilyIPv6},
		{"/proc/net/udp", hostfacts.NetworkUDP, hostfacts.FamilyIPv4}, {"/proc/net/udp6", hostfacts.NetworkUDP, hostfacts.FamilyIPv6},
	} {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		data, used, err := readBounded(spec.path, remaining)
		remaining -= used
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				result.ReasonCodes = append(result.ReasonCodes, "proc_socket_table_missing")
				continue
			}
			return result, err
		}
		if remaining <= 0 {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "inventory_truncated")
			break
		}
		rows := parseProcNet(data, spec.network, spec.family)
		for _, row := range rows {
			if len(result.Sockets) >= limits.MaxSockets {
				result.Truncated = true
				result.ReasonCodes = append(result.ReasonCodes, "inventory_truncated")
				break
			}
			result.Sockets = append(result.Sockets, row)
		}
	}
	owners, truncated := socketOwners(ctx, result.Sockets, limits.MaxCandidatePIDs)
	if truncated {
		result.Truncated = true
		result.ReasonCodes = append(result.ReasonCodes, "pid_inventory_truncated")
	}
	for index := range result.Sockets {
		result.Sockets[index].Processes = owners[result.Sockets[index].Inode]
	}
	return result, nil
}

func readBounded(path string, limit int64) ([]byte, int64, error) {
	if limit <= 0 {
		return nil, 0, io.ErrShortBuffer
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	used := int64(len(data))
	if used > limit {
		data = data[:limit]
		return data, limit, nil
	}
	return data, used, err
}

func parseProcNet(data []byte, network hostfacts.Network, family hostfacts.Family) []RawSocket {
	result := []RawSocket{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		if network == hostfacts.NetworkTCP && fields[3] != "0A" {
			continue
		}
		if network == hostfacts.NetworkUDP && !procRemoteUnspecified(fields[2]) {
			// Connected UDP client sockets use transient local ports and are not
			// listener surfaces. Retaining them would create churned IDs and could
			// crowd real listeners out of the bounded inventory.
			continue
		}
		address, port, ok := parseProcAddress(fields[1], family)
		if !ok || port == 0 {
			continue
		}
		result = append(result, RawSocket{Network: network, Family: family, Bind: address, Port: port, Protocol: string(network), Inode: fields[9]})
	}
	return result
}

func procRemoteUnspecified(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || strings.Trim(parts[0], "0") != "" {
		return false
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	return err == nil && port == 0
}

func parseProcAddress(value string, family hostfacts.Family) (string, uint16, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, false
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", 0, false
	}
	if family == hostfacts.FamilyIPv4 && len(raw) == 4 {
		raw[0], raw[3] = raw[3], raw[0]
		raw[1], raw[2] = raw[2], raw[1]
	}
	if family == hostfacts.FamilyIPv6 && len(raw) == 16 {
		for i := 0; i < 16; i += 4 {
			raw[i], raw[i+3] = raw[i+3], raw[i]
			raw[i+1], raw[i+2] = raw[i+2], raw[i+1]
		}
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return "", 0, false
	}
	return addr.Unmap().String(), uint16(port64), true
}

func socketOwners(ctx context.Context, sockets []RawSocket, maxPIDs int) (map[string][]RawProcess, bool) {
	wanted := make(map[string]struct{}, len(sockets))
	for _, socket := range sockets {
		if socket.Inode != "" {
			wanted[socket.Inode] = struct{}{}
		}
	}
	result := make(map[string][]RawProcess)
	seenOwner := make(map[string]map[int]struct{})
	entryNames, directoryTruncated, err := readDirNamesBounded("/proc", maxPIDs+256)
	if err != nil {
		return result, false
	}
	pids := make([]int, 0)
	for _, name := range entryNames {
		if pid, err := strconv.Atoi(name); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	truncated := directoryTruncated
	if len(pids) > maxPIDs {
		pids = pids[:maxPIDs]
		truncated = true
	}
	for _, pid := range pids {
		if ctx.Err() != nil {
			break
		}
		fdNames, fdTruncated, err := readDirNamesBounded(fmt.Sprintf("/proc/%d/fd", pid), 4096)
		if err != nil {
			continue
		}
		if fdTruncated {
			truncated = true
		}
		process, loaded := RawProcess{}, false
		for _, fd := range fdNames {
			link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", fd))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if _, ok := wanted[inode]; !ok {
				continue
			}
			if !loaded {
				process = readProcess(pid)
				loaded = true
			}
			if seenOwner[inode] == nil {
				seenOwner[inode] = make(map[int]struct{})
			}
			if _, exists := seenOwner[inode][pid]; exists {
				continue
			}
			seenOwner[inode][pid] = struct{}{}
			result[inode] = append(result[inode], process)
		}
	}
	return result, truncated
}

func readDirNamesBounded(path string, limit int) ([]string, bool, error) {
	if limit < 1 {
		return nil, true, nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer directory.Close()
	names, err := directory.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	truncated := len(names) > limit
	if truncated {
		names = names[:limit]
	}
	return names, truncated, nil
}

func readProcess(pid int) RawProcess {
	result := RawProcess{PID: pid, UID: -1}
	if stat, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
			result.UID = int(sys.Uid)
		}
	}
	if data, _, err := readBounded(fmt.Sprintf("/proc/%d/stat", pid), 8192); err == nil {
		result.StartTime = procStartTime(string(data))
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		if stat, statErr := os.Stat(exe); statErr == nil {
			result.ExecutableToken = fmt.Sprintf("%s|%d|%d", exe, stat.Size(), stat.ModTime().UnixNano())
		}
	}
	if data, _, err := readBounded(fmt.Sprintf("/proc/%d/cgroup", pid), 4096); err == nil {
		parseCgroup(string(data), &result)
	}
	return result
}

func procStartTime(value string) string {
	closing := strings.LastIndex(value, ")")
	if closing < 0 || closing+1 >= len(value) {
		return ""
	}
	// Fields after the comm closing parenthesis start at field 3 (state), so
	// field 22 (starttime) is index 19 in this suffix. The comm itself may
	// contain spaces or parentheses and must not be split with Fields.
	fields := strings.Fields(value[closing+1:])
	if len(fields) <= 19 {
		return ""
	}
	for _, r := range fields[19] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return fields[19]
}

func parseCgroup(value string, result *RawProcess) {
	for _, line := range strings.Split(value, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		for _, segment := range strings.Split(path, "/") {
			if strings.HasSuffix(segment, ".service") && len(segment) <= 128 {
				result.SystemdUnit = segment
			}
			if strings.Contains(segment, "docker") || strings.Contains(segment, "libpod") || strings.Contains(segment, "containerd") {
				result.ContainerCgroup = segment
			}
		}
	}
}
