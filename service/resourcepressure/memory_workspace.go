package resourcepressure

import (
	"context"
	"errors"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryWorkspace is an on-demand budget for buffered input, not a promise
// about application RSS. Keep room for decoding and normalizing the input,
// concurrent Host services, and other allocations not charged to this limit.
func (m *Manager) MemoryWorkspace(ctx context.Context, fallback, ceiling int) int {
	if ctx.Err() != nil || m.Current().State == domain.StateCritical {
		return 0
	}
	available := ^uint64(0)
	known := false
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil && memory.Total > 0 {
		available, known = memory.Available, true
	}
	if runtime.GOOS == "linux" {
		remaining, err := cgroupWorkspaceHeadroom(os.ReadFile)
		if err == nil {
			available = min(available, remaining)
		} else {
			known = false
		}
	}
	return workspaceSize(available, known, fallback, ceiling)
}

func workspaceSize(available uint64, known bool, fallback, ceiling int) int {
	if !known {
		ceiling = min(ceiling, fallback)
	}
	// ZIP, decoded values, canonical copies, and serialization can coexist.
	return int(min(available/16, uint64(max(ceiling, 0))))
}

func cgroupWorkspaceHeadroom(readFile func(string) ([]byte, error)) (uint64, error) {
	const membership = "/proc/self/cgroup"
	before, err := readBoundedCgroupFile(readFile, membership, 64<<10)
	if err != nil {
		return 0, err
	}
	leaf, err := parseUnifiedCgroupLeaf(before)
	if err != nil {
		return 0, err
	}
	info, err := readBoundedCgroupFile(readFile, "/proc/self/mountinfo", mountevidence.MaxMountInfo)
	if err != nil {
		return 0, err
	}
	points, err := mountevidence.FilesystemMountPoints(info, "cgroup2")
	if err != nil || len(points) != 1 {
		return 0, mountevidence.ErrUnavailable
	}
	root := points[0]
	mount, err := mountevidence.ParseUniqueMount(info, root)
	if err != nil || mount.Root != "/" || mount.MountPoint != root || mount.Filesystem != "cgroup2" {
		return 0, mountevidence.ErrUnavailable
	}
	remaining := ^uint64(0)
	for dir := path.Join(root, strings.TrimPrefix(leaf, "/")); ; dir = path.Dir(dir) {
		limit := ^uint64(0)
		for _, name := range []string{"memory.max", "memory.high"} {
			raw, readErr := readBoundedCgroupFile(readFile, path.Join(dir, name), 128)
			value := strings.TrimSpace(string(raw))
			if errors.Is(readErr, os.ErrNotExist) || readErr == nil && value == "max" {
				continue
			}
			bytes, parseErr := strconv.ParseUint(value, 10, 64)
			if readErr != nil || parseErr != nil {
				return 0, errors.New("cgroup memory limit is unreadable")
			}
			limit = min(bytes, limit)
		}
		if limit != ^uint64(0) {
			raw, readErr := readBoundedCgroupFile(readFile, path.Join(dir, "memory.current"), 128)
			current, parseErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
			if readErr != nil || parseErr != nil {
				return 0, errors.New("cgroup memory charge is unreadable")
			}
			remaining = min(remaining, max(limit, current)-current)
		}
		if dir == root {
			break
		}
	}
	after, err := readBoundedCgroupFile(readFile, membership, 64<<10)
	if err != nil || string(after) != string(before) {
		return 0, errors.New("cgroup membership changed during memory observation")
	}
	return remaining, nil
}
