//go:build linux

package processevidence

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxProcessDescriptors = 4096

// ObserveDescriptorSnapshot reads a process descriptor directory through an
// explicit upper bound. Readlink failure is treated as an unstable snapshot,
// rather than silently accepting a descriptor set assembled across races.
func ObserveDescriptorSnapshot(pid int) (DescriptorSnapshot, error) {
	if pid <= 0 {
		return DescriptorSnapshot{}, errors.New("process descriptor target is invalid")
	}
	root := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	directory, err := os.Open(root)
	if err != nil {
		return DescriptorSnapshot{}, errors.New("process descriptor inventory is unavailable")
	}
	defer directory.Close()
	names, err := directory.Readdirnames(maxProcessDescriptors + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return DescriptorSnapshot{}, errors.New("process descriptor inventory is unavailable")
	}
	if len(names) > maxProcessDescriptors {
		return DescriptorSnapshot{}, ErrDescriptorInventoryBound
	}
	result := DescriptorSnapshot{Count: len(names), SocketInodes: make(map[int]string)}
	for _, name := range names {
		fd, parseErr := strconv.Atoi(name)
		if parseErr != nil || fd < 0 {
			return DescriptorSnapshot{}, errors.New("process descriptor inventory is malformed")
		}
		target, linkErr := os.Readlink(filepath.Join(root, name))
		if linkErr != nil {
			return DescriptorSnapshot{}, errors.New("process descriptor inventory changed while observing")
		}
		inode, ok := socketInode(target)
		if ok {
			result.SocketInodes[fd] = inode
		}
	}
	return result, nil
}

func socketInode(target string) (string, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return "", false
	}
	inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
	if inode == "" {
		return "", false
	}
	for _, value := range inode {
		if value < '0' || value > '9' {
			return "", false
		}
	}
	return inode, true
}
