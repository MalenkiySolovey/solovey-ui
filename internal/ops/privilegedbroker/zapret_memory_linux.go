//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/shirou/gopsutil/v4/mem"
)

var errZapretMemoryBudget = errors.New("installed Zapret replacement memory headroom is insufficient")
var errZapretMemoryEvidence = errors.New("installed Zapret memory headroom could not be verified")

const (
	zapretCgroupRoot = "/sys/fs/cgroup"
	// The pinned nfqws2 requests Q_RCVBUF=1 MiB. Linux doubles the
	// socket buffer for accounting. This is not a bound on queued skbs.
	zapretQueueBufferReserve = 2 * (1 << 20)
)

type zapretMemoryHeadroom struct {
	source string
	bytes  uint64
}

type zapretMemoryLimits struct {
	hard     zapretMemoryHeadroom
	throttle zapretMemoryHeadroom
}

func checkZapretMemoryBudget(ctx context.Context, args []string, activePID int) error {
	host, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return fmt.Errorf("%w: Host MemAvailable is unavailable", errZapretMemoryEvidence)
	}
	limits := zapretMemoryLimits{hard: zapretMemoryHeadroom{source: "Host MemAvailable", bytes: host.Available}}
	if cgroup2Filesystem(zapretCgroupRoot) {
		mount, mountErr := mountevidence.ObserveUniqueMount(zapretCgroupRoot)
		before := processevidence.SelfCgroup()
		leaf, ok := processevidence.UnifiedCgroup(before)
		if mountErr != nil || mount.MountPoint != zapretCgroupRoot || mount.Root != "/" || !ok {
			return fmt.Errorf("%w: broker cgroup v2 membership or mount is unavailable", errZapretMemoryEvidence)
		}
		limits, err = zapretCgroupHeadroom(limits.hard, zapretCgroupRoot, leaf, readZapretCgroupNumber)
		after := processevidence.SelfCgroup()
		if err != nil {
			return fmt.Errorf("%w: %s", errZapretMemoryEvidence, err)
		}
		if before.CgroupRevision != after.CgroupRevision {
			return fmt.Errorf("%w: broker cgroup v2 membership changed", errZapretMemoryEvidence)
		}
	}
	// The active engine is still charged during preflight. Reserve an
	// additional working set before risking a restart; for a cold start the
	// pinned executable size is only a lower-bound proxy. This is a capacity
	// check at admission, not a prediction of peak packet-processing memory.
	image, err := os.Stat(zapretEnginePath)
	if err != nil || image.Size() <= 0 {
		return fmt.Errorf("%w: pinned engine image size is unavailable", errZapretMemoryEvidence)
	}
	workingSet := uint64(image.Size())
	if activePID > 0 {
		peak, peakErr := processevidence.ResidentHighWater(activePID)
		if peakErr != nil {
			return fmt.Errorf("%w: active engine memory high-water mark is unavailable", errZapretMemoryEvidence)
		}
		workingSet = max(workingSet, peak)
	}
	return admitZapretMemory(limits, workingSet, args)
}

func admitZapretMemory(limits zapretMemoryLimits, workingSet uint64, args []string) error {
	reserve := uint64(zapretQueueBufferReserve)
	for _, arg := range args {
		reserve += uint64(len(arg)) + 1 + strconv.IntSize/8
	}
	for _, available := range []zapretMemoryHeadroom{limits.hard, limits.throttle} {
		// Compare without adding the working set to a potentially full uint64.
		if available.source == "" || workingSet <= available.bytes && reserve <= available.bytes-workingSet {
			continue
		}
		detail := ""
		if available == limits.throttle {
			detail = " before throttling"
		}
		// Keep the reason within the broker's public response limit.
		required := workingSet + min(reserve, ^uint64(0)-workingSet)
		return fmt.Errorf("%w: %s leaves %d bytes%s; need %d bytes",
			errZapretMemoryBudget, available.source, available.bytes, detail, required)
	}
	return nil
}

func zapretCgroupHeadroom(host zapretMemoryHeadroom, root, leaf string, read func(string) (string, error)) (zapretMemoryLimits, error) {
	if !filepath.IsAbs(leaf) || filepath.Clean(leaf) != leaf {
		return zapretMemoryLimits{}, errors.New("noncanonical cgroup membership")
	}
	result := zapretMemoryLimits{hard: host}
	for dir := filepath.Join(root, strings.TrimPrefix(leaf, "/")); dir != root; dir = filepath.Dir(dir) {
		var current uint64
		measured := false
		for i, file := range []string{"memory.max", "memory.high"} {
			raw, err := read(filepath.Join(dir, file))
			if errors.Is(err, os.ErrNotExist) || err == nil && raw == "max" {
				continue
			}
			bytes, parseErr := strconv.ParseUint(raw, 10, 64)
			if err != nil || parseErr != nil {
				return zapretMemoryLimits{}, errors.New("finite cgroup memory limit is unreadable")
			}
			if !measured {
				raw, err = read(filepath.Join(dir, "memory.current"))
				current, parseErr = strconv.ParseUint(raw, 10, 64)
				if err != nil || parseErr != nil {
					return zapretMemoryLimits{}, errors.New("current cgroup memory charge is unreadable")
				}
				measured = true
			}
			remaining := max(bytes, current) - current
			target := &result.hard
			if i != 0 {
				target = &result.throttle
			}
			if target.source == "" || remaining < target.bytes {
				*target = zapretMemoryHeadroom{source: "broker cgroup v2 " + file, bytes: remaining}
			}
		}
	}
	return result, nil
}

func readZapretCgroupNumber(path string) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- kernel-reported membership below the verified cgroup v2 mount.
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65))
	if len(raw) > 64 {
		return "", errors.New("cgroup memory counter is unbounded")
	}
	return strings.TrimSpace(string(raw)), err
}
