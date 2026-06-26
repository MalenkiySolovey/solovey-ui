package update

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

type UpdateStage string

const (
	UpdateStageIdle        UpdateStage = "idle"
	UpdateStageDownloading UpdateStage = "downloading"
	UpdateStageVerifying   UpdateStage = "verifying"
	UpdateStageApplying    UpdateStage = "applying"
	UpdateStageRestarting  UpdateStage = "restarting"
	UpdateStageFailed      UpdateStage = "failed"
)

type UpdateJob struct {
	ID          string      `json:"id"`
	Channel     string      `json:"channel"`
	FromVersion string      `json:"fromVersion"`
	ToVersion   string      `json:"toVersion"`
	Stage       UpdateStage `json:"stage"`
	Error       string      `json:"error,omitempty"`
	StartedAt   int64       `json:"startedAt"`
	Initiator   string      `json:"initiator,omitempty"`
}

var ErrUpdateInProgress = errors.New("an update is already in progress")

type ManagerOptions struct {
	CurrentVersion func() string
	PipelineDeps   func() PipelineDeps
	Pipeline       func(ReleaseTarget, PipelineDeps, func(UpdateStage)) error
	TerminalAudit  func(UpdateJob, string, string)
	Exit           func()
	Now            func() time.Time
}

type Manager struct {
	mu     sync.Mutex
	job    *UpdateJob
	active bool
	opt    ManagerOptions
}

func NewManager(options ManagerOptions) *Manager {
	if options.CurrentVersion == nil {
		options.CurrentVersion = configidentity.GetVersion
	}
	if options.PipelineDeps == nil {
		options.PipelineDeps = DefaultPipelineDeps
	}
	if options.Pipeline == nil {
		options.Pipeline = ApplyPipeline
	}
	if options.TerminalAudit == nil {
		options.TerminalAudit = func(UpdateJob, string, string) {}
	}
	if options.Exit == nil {
		options.Exit = func() { os.Exit(1) }
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Manager{opt: options}
}

func (m *Manager) Status() UpdateJob {
	if m == nil {
		return UpdateJob{Stage: UpdateStageIdle}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job == nil {
		return UpdateJob{Stage: UpdateStageIdle}
	}
	return *m.job
}

func (m *Manager) InProgress() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *Manager) Apply(target ReleaseTarget, initiator string) error {
	if m == nil {
		return errors.New("update manager is not configured")
	}
	if normalized := versionpolicy.NormalizeVersion(target.Version); normalized == "" {
		return errors.New("invalid update target version")
	}
	if comparison, ok := versionpolicy.CompareVersions(target.Version, m.opt.CurrentVersion()); !ok || comparison <= 0 {
		return ErrNotNewer
	}
	m.mu.Lock()
	if m.active {
		m.mu.Unlock()
		return ErrUpdateInProgress
	}
	now := m.opt.Now()
	m.active = true
	m.job = &UpdateJob{
		ID: fmt.Sprintf("upd-%d", now.UnixNano()), Channel: target.Channel,
		FromVersion: m.opt.CurrentVersion(), ToVersion: target.Version,
		Stage: UpdateStageDownloading, StartedAt: now.Unix(), Initiator: initiator,
	}
	m.mu.Unlock()
	go m.run(target)
	return nil
}

func (m *Manager) run(target ReleaseTarget) {
	deps := m.opt.PipelineDeps()
	if err := m.opt.Pipeline(target, deps, m.setStage); err != nil {
		m.fail(err, deps.ExecPath)
		return
	}
	m.setStage(UpdateStageRestarting)
	if err := writePendingMarker(deps.ExecPath); err != nil {
		m.fail(fmt.Errorf("rollback marker could not be written after apply: %w", err), deps.ExecPath)
		return
	}
	job := m.Status()
	m.opt.TerminalAudit(job, "applied", "")
	logger.Info("panel update applied: ", target.Version, "; restarting into new binary")
	m.opt.Exit()
}

func (m *Manager) setStage(stage UpdateStage) {
	m.mu.Lock()
	if m.job != nil {
		m.job.Stage = stage
	}
	m.mu.Unlock()
}

func (m *Manager) fail(updateErr error, executable string) {
	logger.Warning("panel update failed: ", updateErr)
	if executable != "" {
		if err := RestoreBackup(executable); err != nil && !os.IsNotExist(err) {
			logger.Warning("panel update backup restore failed: ", err)
		}
	}
	m.mu.Lock()
	job := UpdateJob{}
	if m.job != nil {
		m.job.Stage = UpdateStageFailed
		m.job.Error = redact.String(updateErr.Error())
		job = *m.job
	}
	m.active = false
	m.mu.Unlock()
	m.opt.TerminalAudit(job, "failed", updateErr.Error())
}
