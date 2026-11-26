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

func checkZapretMemoryBudget(ctx context.Context, args []string, activePID int) error {
	host, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return fmt.Errorf("%w: Host MemAvailable is unavailable", errZapretMemoryEvidence)
	}
	available := zapretMemoryHeadroom{source: "Host MemAvailable", bytes: host.Available}
	if cgroup2Filesystem(zapretCgroupRoot) {
		mount, mountErr := mountevidence.ObserveUniqueMount(zapretCgroupRoot)
		before := processevidence.SelfCgroup()
		leaf, ok := processevidence.UnifiedCgroup(before)
		if mountErr != nil || mount.MountPoint != zapretCgroupRoot || mount.Root != "/" || !ok {
			return fmt.Errorf("%w: broker cgroup v2 membership or mount is unavailable", errZapretMemoryEvidence)
		}
		available, err = zapretCgroupHeadroom(available, zapretCgroupRoot, leaf, readZapretCgroupNumber)
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
	return admitZapretMemory(available, workingSet, args)
}

func admitZapretMemory(available zapretMemoryHeadroom, workingSet uint64, args []string) error {
	reserve := uint64(zapretQueueBufferReserve)
	for _, arg := range args {
		reserve += uint64(len(arg)) + 1 + strconv.IntSize/8
	}
	// Compare without adding the working set to a potentially full uint64.
	if workingSet > available.bytes || reserve > available.bytes-workingSet {
		return fmt.Errorf("%w: %s leaves %d bytes; reserve %d bytes for the engine plus %d bytes for arguments and the receive buffer",
			errZapretMemoryBudget, available.source, available.bytes, workingSet, reserve)
	}
	return nil
}

func zapretCgroupHeadroom(available zapretMemoryHeadroom, root, leaf string, read func(string) (string, error)) (zapretMemoryHeadroom, error) {
	if !filepath.IsAbs(leaf) || filepath.Clean(leaf) != leaf {
		return zapretMemoryHeadroom{}, errors.New("noncanonical cgroup membership")
	}
	for dir := filepath.Join(root, strings.TrimPrefix(leaf, "/")); dir != root; dir = filepath.Dir(dir) {
		var limit zapretMemoryHeadroom
		for _, file := range []string{"memory.max", "memory.high"} {
			raw, err := read(filepath.Join(dir, file))
			if errors.Is(err, os.ErrNotExist) || err == nil && raw == "max" {
				continue
			}
			bytes, parseErr := strconv.ParseUint(raw, 10, 64)
			if err != nil || parseErr != nil {
				return zapretMemoryHeadroom{}, errors.New("finite cgroup memory limit is unreadable")
			}
			if limit.source == "" || bytes < limit.bytes {
				limit = zapretMemoryHeadroom{source: file, bytes: bytes}
			}
		}
		if limit.source == "" {
			continue
		}
		raw, err := read(filepath.Join(dir, "memory.current"))
		current, parseErr := strconv.ParseUint(raw, 10, 64)
		if err != nil || parseErr != nil {
			return zapretMemoryHeadroom{}, errors.New("current cgroup memory charge is unreadable")
		}
		remaining := max(limit.bytes, current) - current
		if remaining < available.bytes {
			available = zapretMemoryHeadroom{source: "broker cgroup v2 " + limit.source, bytes: remaining}
		}
	}
	return available, nil
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
