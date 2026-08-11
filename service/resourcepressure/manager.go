package resourcepressure

import (
	"bufio"
	"context"
	"errors"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"gorm.io/gorm"
)

const (
	PollInterval            = 15 * time.Second
	MaxPersistedTransitions = 512
)

type Collector interface {
	Collect(context.Context, time.Time) []domain.Signal
}

type Repository struct{ DB func() *gorm.DB }

type Manager struct {
	mu        sync.RWMutex
	observeMu sync.Mutex
	evaluator *domain.Evaluator
	collector Collector
	repo      Repository
	now       func() time.Time
	current   domain.Snapshot
	started   bool
}

var shared = newDefaultManager()

func Shared() *Manager { return shared }

func newDefaultManager() *Manager {
	evaluator, _ := domain.NewEvaluator(domain.DefaultThresholds())
	return &Manager{evaluator: evaluator, collector: SystemCollector{}, repo: Repository{DB: dbsqlite.DB},
		now: time.Now, current: domain.Snapshot{State: domain.StateUnknown, ReasonCodes: []string{"pressure_not_observed"}}}
}

func NewManager(evaluator *domain.Evaluator, collector Collector, repository Repository) *Manager {
	if evaluator == nil {
		evaluator, _ = domain.NewEvaluator(domain.DefaultThresholds())
	}
	if collector == nil {
		collector = SystemCollector{}
	}
	return &Manager{evaluator: evaluator, collector: collector, repo: repository, now: time.Now,
		current: domain.Snapshot{State: domain.StateUnknown, ReasonCodes: []string{"pressure_not_observed"}}}
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	_ = m.Observe(ctx)
	go func() {
		ticker := time.NewTicker(PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.mu.Lock()
				m.started = false
				m.mu.Unlock()
				return
			case <-ticker.C:
				observeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				_ = m.Observe(observeCtx)
				cancel()
			}
		}
	}()
}

func (m *Manager) Observe(ctx context.Context) error {
	if m == nil || m.evaluator == nil || m.collector == nil {
		return errors.New("resource pressure manager is unavailable")
	}
	m.observeMu.Lock()
	defer m.observeMu.Unlock()
	now := m.now()
	snapshot := m.evaluator.Evaluate(now, m.collector.Collect(ctx, now))
	m.mu.Lock()
	previous := m.current
	m.current = snapshot
	m.mu.Unlock()
	if snapshot.State != previous.State || snapshot.Revision != previous.Revision {
		return m.persist(ctx, previous, snapshot)
	}
	return nil
}

func (m *Manager) Current() domain.Snapshot {
	if m == nil {
		return domain.Snapshot{State: domain.StateUnknown, ReasonCodes: []string{"pressure_manager_unavailable"}}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	copySnapshot := m.current
	copySnapshot.Signals = append([]domain.Signal(nil), m.current.Signals...)
	copySnapshot.ReasonCodes = append([]string(nil), m.current.ReasonCodes...)
	if copySnapshot.ObservedAt > 0 && m.now().After(time.Unix(copySnapshot.ObservedAt, 0).Add(domain.DefaultFreshness)) {
		copySnapshot.PreviousState, copySnapshot.State = copySnapshot.State, domain.StateUnknown
		copySnapshot.ReasonCodes = []string{"pressure_snapshot_stale"}
	}
	return copySnapshot
}

func (m *Manager) Admission(pressureClass string) domain.Admission {
	return domain.Decide(m.Current().State, pressureClass)
}

func (m *Manager) persist(ctx context.Context, previous, current domain.Snapshot) error {
	if m.repo.DB == nil || m.repo.DB() == nil {
		return nil
	}
	db := m.repo.DB().WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		row := model.ResourcePressureState{Scope: "panel", State: string(current.State), PreviousState: string(current.PreviousState),
			ReasonCode: firstReason(current.ReasonCodes), ObservationDigest: current.ObservationDigest, Revision: current.Revision,
			ObservedAt: current.ObservedAt, UpdatedAt: current.ObservedAt}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if previous.State != current.State {
			if err := tx.Create(&model.ResourcePressureTransition{FromState: string(previous.State), ToState: string(current.State),
				ReasonCode: firstReason(current.ReasonCodes), ObservationDigest: current.ObservationDigest,
				Revision: current.Revision, CreatedAt: current.ObservedAt}).Error; err != nil {
				return err
			}
			return prunePersistedTransitions(tx)
		}
		return nil
	})
}

func prunePersistedTransitions(tx *gorm.DB) error {
	if tx == nil {
		return errors.New("resource pressure transition repository is unavailable")
	}
	var floor uint64
	if err := tx.Model(&model.ResourcePressureTransition{}).Select("sequence").
		Order("sequence DESC").Offset(MaxPersistedTransitions - 1).Limit(1).Scan(&floor).Error; err != nil {
		return err
	}
	if floor == 0 {
		return nil
	}
	return tx.Where("sequence < ?", floor).Delete(&model.ResourcePressureTransition{}).Error
}

func transitionRetentionCutoff(sequences []uint64, keep int) uint64 {
	if keep <= 0 || len(sequences) <= keep {
		return 0
	}
	copySequences := append([]uint64(nil), sequences...)
	sort.Slice(copySequences, func(i, j int) bool { return copySequences[i] > copySequences[j] })
	return copySequences[keep-1]
}

func firstReason(reasons []string) string {
	if len(reasons) == 0 {
		return "pressure_reason_unavailable"
	}
	return reasons[0]
}

type SystemCollector struct{}

func (SystemCollector) Collect(ctx context.Context, now time.Time) []domain.Signal {
	result := make([]domain.Signal, 0, 24)
	expires := now.Add(domain.DefaultFreshness).Unix()
	add := func(signal domain.Signal) {
		signal.ObservedAt, signal.ExpiresAt = now.Unix(), expires
		result = append(result, signal)
	}
	if usage, err := disk.Usage(configstorage.GetDBFolderPath()); err == nil && usage.Total > 0 {
		add(domain.Signal{ID: "filesystem.data.free_ratio", Status: domain.ProviderSupported, Value: float64(usage.Free) / float64(usage.Total), Unit: "ratio"})
		add(domain.Signal{ID: "filesystem.data.free_bytes", Status: domain.ProviderSupported, Value: float64(usage.Free), Unit: "bytes"})
		if usage.InodesTotal > 0 {
			add(domain.Signal{ID: "filesystem.data.free_inode_ratio", Status: domain.ProviderSupported,
				Value: float64(usage.InodesFree) / float64(usage.InodesTotal), Unit: "ratio"})
		} else {
			add(domain.Signal{ID: "filesystem.data.free_inode_ratio", Status: domain.ProviderUnsupported, ReasonCode: "filesystem_data_inode_limit_unavailable"})
		}
	} else {
		add(domain.Signal{ID: "filesystem.data.free_ratio", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_data_unavailable"})
		add(domain.Signal{ID: "filesystem.data.free_bytes", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_data_unavailable"})
		add(domain.Signal{ID: "filesystem.data.free_inode_ratio", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_data_unavailable"})
	}
	if usage, err := disk.Usage(os.TempDir()); err == nil && usage.Total > 0 {
		add(domain.Signal{ID: "filesystem.temp.free_ratio", Status: domain.ProviderSupported, Value: float64(usage.Free) / float64(usage.Total), Unit: "ratio"})
		add(boundedFilesystemFreeBytesSignal(
			"filesystem.temp.free_bytes", "filesystem_temp_absolute_bytes_inapplicable", usage.Total, usage.Free,
		))
		if usage.InodesTotal > 0 {
			add(domain.Signal{ID: "filesystem.temp.free_inode_ratio", Status: domain.ProviderSupported,
				Value: float64(usage.InodesFree) / float64(usage.InodesTotal), Unit: "ratio"})
		} else {
			add(domain.Signal{ID: "filesystem.temp.free_inode_ratio", Status: domain.ProviderUnsupported, ReasonCode: "filesystem_temp_inode_limit_unavailable"})
		}
	} else {
		add(domain.Signal{ID: "filesystem.temp.free_ratio", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_temp_unavailable"})
		add(domain.Signal{ID: "filesystem.temp.free_bytes", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_temp_unavailable"})
		add(domain.Signal{ID: "filesystem.temp.free_inode_ratio", Status: domain.ProviderUnavailable, ReasonCode: "filesystem_temp_unavailable"})
	}
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil && memory.Total > 0 {
		add(domain.Signal{ID: "memory.used_ratio", Status: domain.ProviderSupported, Value: float64(memory.Used) / float64(memory.Total), Unit: "ratio"})
	} else {
		add(domain.Signal{ID: "memory.used_ratio", Status: domain.ProviderUnavailable, ReasonCode: "memory_observation_unavailable"})
	}
	walPath := configstorage.GetDBPath() + "-wal"
	if info, err := os.Stat(walPath); err == nil {
		add(domain.Signal{ID: "sqlite.wal.bytes", Status: domain.ProviderSupported, Value: float64(info.Size()), Unit: "bytes"})
	} else if errors.Is(err, os.ErrNotExist) {
		add(domain.Signal{ID: "sqlite.wal.bytes", Status: domain.ProviderSupported, Value: 0, Unit: "bytes"})
	} else {
		add(domain.Signal{ID: "sqlite.wal.bytes", Status: domain.ProviderUnavailable, ReasonCode: "sqlite_wal_unavailable"})
	}
	add(domain.Signal{ID: "sqlite.busy.rate", Status: domain.ProviderUnavailable, ReasonCode: "sqlite_busy_rate_not_instrumented"})
	add(domain.Signal{ID: "process.goroutines", Status: domain.ProviderSupported, Value: float64(runtime.NumGoroutine()), Unit: "count"})
	if current, err := process.NewProcessWithContext(ctx, int32(os.Getpid())); err == nil {
		if count, fdErr := current.NumFDsWithContext(ctx); fdErr == nil {
			add(domain.Signal{ID: "process.fd.count", Status: domain.ProviderSupported, Value: float64(count), Unit: "count"})
			if limit, limitErr := processOpenFileLimit(); limitErr == nil && limit > 0 {
				add(domain.Signal{ID: "process.fd.used_ratio", Status: domain.ProviderSupported, Value: float64(count) / float64(limit), Unit: "ratio"})
			} else {
				add(domain.Signal{ID: "process.fd.used_ratio", Status: domain.ProviderUnsupported, ReasonCode: "process_fd_limit_unavailable"})
			}
		} else {
			add(domain.Signal{ID: "process.fd.count", Status: domain.ProviderUnsupported, ReasonCode: "process_fd_ratio_unsupported"})
			add(domain.Signal{ID: "process.fd.used_ratio", Status: domain.ProviderUnsupported, ReasonCode: "process_fd_ratio_unsupported"})
		}
	} else {
		add(domain.Signal{ID: "process.fd.count", Status: domain.ProviderUnavailable, ReasonCode: "process_fd_observation_unavailable"})
		add(domain.Signal{ID: "process.fd.used_ratio", Status: domain.ProviderUnavailable, ReasonCode: "process_fd_observation_unavailable"})
	}
	if runtime.GOOS == "linux" {
		add(readCgroupMemory(now))
		add(readPSIMemory(now))
	} else {
		add(domain.Signal{ID: "cgroup.memory.used_ratio", Status: domain.ProviderUnsupported, ReasonCode: "cgroup_not_supported"})
		add(domain.Signal{ID: "psi.memory.some_avg10", Status: domain.ProviderUnsupported, ReasonCode: "psi_not_supported"})
	}
	for _, id := range []string{"http.active", "audit.queue.used_ratio", "operations.heavy.active", "websocket.active",
		"background.tasks.active", "artifacts.bytes", "logs.retention.bytes", "docker.posture", "systemd.posture", "host.protection.posture"} {
		add(domain.Signal{ID: id, Status: domain.ProviderUnavailable, ReasonCode: strings.ReplaceAll(id, ".", "_") + "_unavailable"})
	}
	return result
}

func boundedFilesystemFreeBytesSignal(id, boundedReason string, total, free uint64) domain.Signal {
	for _, threshold := range domain.DefaultThresholds() {
		if threshold.ID != id {
			continue
		}
		if float64(total) <= threshold.Warning {
			return domain.Signal{ID: id, Status: domain.ProviderUnsupported, ReasonCode: boundedReason}
		}
		return domain.Signal{ID: id, Status: domain.ProviderSupported, Value: float64(free), Unit: "bytes"}
	}
	return domain.Signal{ID: id, Status: domain.ProviderUnavailable, ReasonCode: "pressure_threshold_unavailable"}
}

func readCgroupMemory(now time.Time) domain.Signal {
	current, currentErr := readUintFile("/sys/fs/cgroup/memory.current")
	maximumRaw, maximumErr := os.ReadFile("/sys/fs/cgroup/memory.max")
	if currentErr != nil || maximumErr != nil || strings.TrimSpace(string(maximumRaw)) == "max" {
		return domain.Signal{ID: "cgroup.memory.used_ratio", Status: domain.ProviderUnavailable, ReasonCode: "cgroup_memory_limit_unavailable"}
	}
	maximum, err := strconv.ParseUint(strings.TrimSpace(string(maximumRaw)), 10, 64)
	if err != nil || maximum == 0 {
		return domain.Signal{ID: "cgroup.memory.used_ratio", Status: domain.ProviderError, ReasonCode: "cgroup_memory_limit_invalid"}
	}
	return domain.Signal{ID: "cgroup.memory.used_ratio", Status: domain.ProviderSupported, Value: float64(current) / float64(maximum), Unit: "ratio"}
}

func readPSIMemory(now time.Time) domain.Signal {
	file, err := os.Open("/proc/pressure/memory")
	if err != nil {
		return domain.Signal{ID: "psi.memory.some_avg10", Status: domain.ProviderUnavailable, ReasonCode: "psi_memory_unavailable"}
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			if raw, ok := strings.CutPrefix(field, "avg10="); ok {
				value, parseErr := strconv.ParseFloat(raw, 64)
				if parseErr == nil {
					return domain.Signal{ID: "psi.memory.some_avg10", Status: domain.ProviderSupported, Value: value, Unit: "percent"}
				}
			}
		}
	}
	return domain.Signal{ID: "psi.memory.some_avg10", Status: domain.ProviderError, ReasonCode: "psi_memory_invalid"}
}

func readUintFile(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

func processOpenFileLimit() (uint64, error) {
	if runtime.GOOS != "linux" {
		return 0, errors.New("process limits are unsupported")
	}
	file, err := os.Open("/proc/self/limits")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "Max open files") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Max open files"))
		if len(fields) == 0 || fields[0] == "unlimited" {
			return 0, errors.New("process open-file limit is unbounded")
		}
		return strconv.ParseUint(fields[0], 10, 64)
	}
	return 0, errors.Join(scanner.Err(), errors.New("process open-file limit is absent"))
}
