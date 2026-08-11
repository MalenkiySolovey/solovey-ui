//go:build linux

package helper

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func platformNginxOwnsListeners(pids []int, expected []NginxListener) error {
	inodes := map[string]bool{}
	for _, pid := range pids {
		entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
		if err != nil {
			return errors.New("nginx listener process identity is unavailable")
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
			if err == nil && strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
			}
		}
	}
	found := map[string]bool{}
	for _, table := range []struct {
		path string
		ipv6 bool
	}{{"/proc/net/tcp", false}, {"/proc/net/tcp6", true}} {
		file, err := os.Open(table.path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 || fields[3] != "0A" || !inodes[fields[9]] {
				continue
			}
			address, port, err := decodeProcAddress(fields[1], table.ipv6)
			if err != nil {
				continue
			}
			for _, listener := range expected {
				wanted, parseErr := netip.ParseAddr(listener.Address)
				if parseErr == nil && listener.Port == port && wanted == address {
					found[listener.Address+":"+strconv.Itoa(listener.Port)] = true
				}
			}
		}
		scanErr := scanner.Err()
		_ = file.Close()
		if scanErr != nil {
			return scanErr
		}
	}
	for _, listener := range expected {
		if !found[listener.Address+":"+strconv.Itoa(listener.Port)] {
			return fmt.Errorf("expected nginx listener ownership mismatch")
		}
	}
	return nil
}

func decodeProcAddress(value string, ipv6 bool) (netip.Addr, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return netip.Addr{}, 0, errors.New("invalid proc address")
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil {
		return netip.Addr{}, 0, err
	}
	if !ipv6 {
		if len(raw) != 4 {
			return netip.Addr{}, 0, errors.New("invalid IPv4")
		}
		return netip.AddrFrom4([4]byte{raw[3], raw[2], raw[1], raw[0]}), int(port64), nil
	}
	if len(raw) != 16 {
		return netip.Addr{}, 0, errors.New("invalid IPv6")
	}
	var address [16]byte
	for block := 0; block < 4; block++ {
		for index := 0; index < 4; index++ {
			address[block*4+index] = raw[block*4+3-index]
		}
	}
	return netip.AddrFrom16(address), int(port64), nil
}
