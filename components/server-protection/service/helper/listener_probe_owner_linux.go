//go:build linux

package helper

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"
)

// probeListenerOwner is retained for the already-fenced port-handoff probe.
// firewall baseline production ownership uses listener.owner.observe instead.
func probeListenerOwner(ctx context.Context, address netip.Addr, port, expectedPID int) (ListenerOwner, error) {
	if expectedPID <= 1 || port < 1 || port > 65535 {
		return "", errors.New("listener probe owner expectation is invalid")
	}
	wantedAddress, err := procAddress(address)
	if err != nil {
		return "", err
	}
	table := "/proc/net/tcp"
	if address.Is6() {
		table = "/proc/net/tcp6"
	}
	file, err := os.Open(table)
	if err != nil {
		return "", err
	}
	defer file.Close()
	wantedTuple := wantedAddress + ":" + fmt.Sprintf("%04X", port)
	inodes := make([]string, 0, 1)
	scanner := bufio.NewScanner(io.LimitReader(file, 8<<20))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for lines := 0; scanner.Scan(); lines++ {
		if lines >= 65536 {
			return "", errors.New("listener table scan exceeded its bound")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 10 && fields[1] == wantedTuple && fields[3] == "0A" && numericString(fields[9]) {
			inodes = append(inodes, fields[9])
		}
	}
	if err := scanner.Err(); err != nil || len(inodes) != 1 {
		return "", errors.New("listener tuple ownership is absent or ambiguous")
	}
	directory, err := os.Open(fmt.Sprintf("/proc/%d/fd", expectedPID))
	if err != nil {
		return "", err
	}
	defer directory.Close()
	names, err := directory.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if len(names) > 4096 {
		return "", errors.New("listener fd scan exceeded its bound")
	}
	sort.Strings(names)
	wantedLink := "socket:[" + inodes[0] + "]"
	owned := false
	for _, name := range names {
		target, readErr := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", expectedPID, name))
		if readErr == nil && target == wantedLink {
			owned = true
			break
		}
	}
	if !owned {
		return "", errors.New("listener socket is not owned by expected process")
	}
	comm, err := readOwnerFile(fmt.Sprintf("/proc/%d/comm", expectedPID), 256)
	if err != nil {
		return "", err
	}
	owner := classifyListenerProcess(string(comm))
	if owner == "" {
		return "", errors.New("listener process class is unknown")
	}
	return owner, nil
}

func procAddress(address netip.Addr) (string, error) {
	address = address.Unmap()
	if address.Is4() {
		bytes := address.As4()
		return strings.ToUpper(hex.EncodeToString([]byte{bytes[3], bytes[2], bytes[1], bytes[0]})), nil
	}
	if !address.Is6() {
		return "", errors.New("listener address family is invalid")
	}
	bytes := address.As16()
	encoded := make([]byte, 16)
	for block := 0; block < 4; block++ {
		for index := 0; index < 4; index++ {
			encoded[block*4+index] = bytes[block*4+3-index]
		}
	}
	return strings.ToUpper(hex.EncodeToString(encoded)), nil
}

func classifyListenerProcess(value string) ListenerOwner {
	switch strings.TrimSpace(value) {
	case "sing-box":
		return ListenerOwnerSingBox
	case "solovey-ui":
		return ListenerOwnerPanel
	default:
		return ""
	}
}
