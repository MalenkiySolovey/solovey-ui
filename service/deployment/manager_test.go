package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeProvider struct {
	current       domain.ProfileID
	verifyFailure bool
	applyFailure  bool
	rollbackFail  bool
	mutations     []string
	now           time.Time
}

func (*fakeProvider) ProviderID() string { return domain.ProviderV1 }
func (*fakeProvider) Capabilities(context.Context) domain.Capabilities {
	result := domain.Capabilities{Observe: domain.Available, Doctor: domain.Available, Migrate: domain.Available, Rollback: domain.Available}
	result.Revision = domain.Revision(result)
	return result
}
func (p *fakeProvider) Observe(context.Context) (domain.Posture, error) {
	return p.posture(p.current), nil
}
func (p *fakeProvider) Doctor(ctx context.Context) (domain.DoctorReport, error) {
	posture, _ := p.Observe(ctx)
	return domain.FinalizeDoctor(domain.DoctorReport{Posture: &posture, Capabilities: p.Capabilities(ctx), GeneratedAt: p.now.Unix()}), nil
}
func (p *fakeProvider) Prepare(_ context.Context, fence FenceV1, target domain.ProfileID) (string, error) {
	p.mutations = append(p.mutations, "prepare:"+string(target)+":"+fence.OperationID)
	return domain.Revision("checkpoint:" + fence.OperationID), nil
}
func (p *fakeProvider) Apply(_ context.Context, _ FenceV1, target domain.ProfileID, _ string) error {
	p.mutations = append(p.mutations, "apply:"+string(target))
	if p.applyFailure {
		return errors.New("apply failed")
	}
	p.current = target
	return nil
}
func (p *fakeProvider) Verify(_ context.Context, _ FenceV1, target domain.ProfileID, _ string) (domain.Posture, error) {
	p.mutations = append(p.mutations, "verify:"+string(target))
	if p.verifyFailure || p.current != target {
		return domain.Posture{}, errors.New("verify failed")
	}
	return p.posture(target), nil
}
func (p *fakeProvider) Rollback(_ context.Context, _ FenceV1, from domain.ProfileID, _ string) (domain.Posture, error) {
	p.mutations = append(p.mutations, "rollback:"+string(from))
	if p.rollbackFail {
		return domain.Posture{}, errors.New("rollback failed")
	}
	p.current = from
	return p.posture(from), nil
}
func (p *fakeProvider) posture(profileID domain.ProfileID) domain.Posture {
	profile, _ := domain.Lookup(profileID)
	result := domain.Posture{Schema: domain.SchemaV1, Profile: profileID, InstalledProfile: profileID, ActiveProfile: profileID, VerifiedProfile: profileID, Runtime: profile.Runtime,
		PanelUID: 1001, PanelGID: 1001, PanelRoot: profile.PanelRoot, BrokerAvailable: profile.BrokerRequired,
		BrokerRevision: domain.Revision("broker"), ServiceRevision: domain.Revision("service:" + string(profileID)),
		DataRevision: domain.Revision("data:" + string(profileID)), HardeningRevision: domain.Revision("hardening:" + string(profileID)),
		ObservedAt: p.now.Unix(), ExpiresAt: p.now.Add(2 * time.Minute).Unix()}
	if profile.PanelRoot {
		result.PanelUID, result.PanelGID = 0, 0
	}
	systemd := fakeSystemdActualState(p.now, result.PanelRoot)
	result.Systemd = &systemd
	domain.SetPostureRevision(&result)
	return result
}

func fakeSystemdActualState(now time.Time, root bool) domain.SystemdActualState {
	user := "solovey-ui"
	if root {
		user = "root"
	}
	facts := domain.SystemdActualState{Schema: domain.SchemaV1, Version: "systemd-257", ManagerBootRevision: domain.Revision("boot"),
		DirectiveSupport: domain.Available, DirectiveCapabilityRevision: domain.Revision("directives"), Unit: "solovey-ui.service",
		FragmentRevision: domain.Revision("fragment"), DropInRevision: domain.Revision([]string{}), UnitFileState: "enabled",
		LoadState: "loaded", ActiveState: "active", SubState: "running", User: user, Group: user, NoNewPrivileges: !root,
		SandboxRevision: domain.Revision("sandbox"), WritePaths: []string{}, ReadOnlyPaths: []string{}, ExecutableRevision: domain.Revision("executable"),
		RuntimeDirectoryRevision: domain.Revision("runtime"), ResourceRevision: domain.Revision("resources"), Restart: "on-failure",
		RestartUSec: "5s", WatchdogUSec: "0", BrokerSocketRevision: domain.Revision("sockets"),
		ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix()}
	facts.Revision = domain.Revision(facts)
	return facts
}

func TestDeploymentMigrationHappyPathPersistsExplicitStates(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-one", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if err != nil || operation.State != domain.StateVerifying || operation.CheckpointRef == "" {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
	state, err := manager.Repository.State(context.Background())
	if err != nil || state.DesiredProfile != string(domain.NativeHardened) || state.GeneratedProfile != string(domain.NativeHardened) ||
		state.InstalledProfile != string(domain.NativeHardened) || state.ActiveProfile != string(domain.NativeHardened) || state.VerifiedProfile != string(domain.NativeHardened) {
		t.Fatalf("persisted state=%#v err=%v", state, err)
	}
	committed, err := manager.Confirm(context.Background(), ConfirmRequest{OperationID: operation.OperationID, ExpectedRevision: operation.Revision})
	if err != nil || committed.State != domain.StateCommitted {
		t.Fatalf("committed=%#v err=%v", committed, err)
	}
	var journalCount int64
	if err := db.Model(&model.DeploymentJournal{}).Where("operation_id = ?", operation.OperationID).Count(&journalCount).Error; err != nil || journalCount < 5 {
		t.Fatalf("journal count=%d err=%v", journalCount, err)
	}
	if strings.Join(provider.mutations, ",") != "prepare:native-hardened:"+operation.OperationID+",apply:native-hardened,verify:native-hardened,verify:native-hardened" {
		t.Fatalf("mutations=%v", provider.mutations)
	}
}

func TestNativeAdvancedProfileFailsClosedWithoutSeparateRuntime(t *testing.T) {
	manager, _, _ := deploymentFixture(t)
	preview, err := manager.Preview(context.Background(), domain.NativeNetworkAdvanced, true)
	if err != nil || preview.Possible || !containsReason(preview.Reasons, "separate_network_runtime_unavailable") {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
}

func TestDeploymentStatusFailsWhenObservedPostureCannotPersist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:deployment-posture-failure?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	now := time.Unix(1_800_000_000, 0).UTC()
	provider := &fakeProvider{current: domain.NativeLegacyRoot, now: now}
	manager := NewManager(NewRepository(db), provider)
	manager.Now = func() time.Time { return now }

	if _, err := manager.Status(context.Background()); err == nil {
		t.Fatal("Status succeeded without durable deployment state storage")
	}
}

func TestDeploymentFailureExecutesOneExactRollback(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	preview, _ := manager.Preview(context.Background(), domain.NativeHardened, true)
	provider.verifyFailure = true
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-rollback", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if err != nil || operation.State != domain.StateRolledBack || provider.current != domain.NativeLegacyRoot {
		t.Fatalf("operation=%#v current=%s err=%v", operation, provider.current, err)
	}
	rollbacks := 0
	for _, mutation := range provider.mutations {
		if strings.HasPrefix(mutation, "rollback:") {
			rollbacks++
		}
	}
	if rollbacks != 1 {
		t.Fatalf("rollback calls=%d mutations=%v", rollbacks, provider.mutations)
	}
}

func TestDeploymentRollbackFailureRequiresManualRecovery(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	preview, _ := manager.Preview(context.Background(), domain.NativeHardened, true)
	provider.verifyFailure, provider.rollbackFail = true, true
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-manual", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if !errors.Is(err, ErrUnsafeMigration) || operation.State != domain.StateManualRecoveryRequired {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestDeploymentManagementEvidenceDriftRollsBackBeforeSwitch(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	calls := 0
	manager.Management = func(context.Context, time.Time) ManagementPreservation {
		calls++
		if calls >= 3 {
			result := ManagementPreservation{EvidenceRevision: domain.Revision("changed-management"), Reasons: []string{"fresh_independent_ssh_recovery_missing"}}
			result.Revision = domain.Revision(result)
			return result
		}
		return readyManagement()
	}
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-management-drift", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if err != nil || operation.State != domain.StateRolledBack || provider.current != domain.NativeLegacyRoot {
		t.Fatalf("operation=%#v current=%s err=%v", operation, provider.current, err)
	}
	if strings.Contains(strings.Join(provider.mutations, ","), "apply:") || !strings.Contains(strings.Join(provider.mutations, ","), "rollback:native-legacy-root") {
		t.Fatalf("management drift mutation sequence=%v", provider.mutations)
	}
}

func TestDeploymentCommitRequiresPanelAndCoreHealth(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-health", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if err != nil || operation.State != domain.StateVerifying {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
	manager.Health = func(context.Context, time.Time) RuntimeHealth {
		result := RuntimeHealth{EvidenceRevision: domain.Revision("failed-health"), Reasons: []string{"core_runtime_health_failed"}}
		result.Revision = domain.Revision(result)
		return result
	}
	rolledBack, err := manager.Confirm(context.Background(), ConfirmRequest{OperationID: operation.OperationID, ExpectedRevision: operation.Revision})
	if err != nil || rolledBack.State != domain.StateRolledBack || provider.current != domain.NativeLegacyRoot {
		t.Fatalf("rolledBack=%#v current=%s err=%v", rolledBack, provider.current, err)
	}
}

func TestDeploymentStartupAndRestoreReconciliationAreFailClosed(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	var err error
	operation := domain.Operation{Schema: domain.SchemaV1, OperationID: "deployment-operation:restart", IdempotencyKey: "deployment-idem-restart",
		State: domain.StateApplying, FromProfile: domain.NativeLegacyRoot, TargetProfile: domain.NativeHardened,
		ExpectedPosture: provider.posture(domain.NativeLegacyRoot).Revision, ExpectedManagement: readyManagement().Revision,
		CheckpointRef: domain.Revision("checkpoint"),
		Revision:      3, CreatedAt: provider.now.Unix(), UpdatedAt: provider.now.Unix()}
	operation.BindingRevision = domain.OperationBinding(operation)
	if err := manager.Repository.Create(context.Background(), operation, "applying_before_restart"); err != nil {
		t.Fatal(err)
	}
	provider.current = domain.NativeHardened
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	operation, err = manager.Repository.ByID(context.Background(), operation.OperationID)
	if err != nil {
		var raw model.DeploymentOperation
		_ = db.Where("operation_id = ?", operation.OperationID).Take(&raw).Error
		t.Fatalf("%v raw=%#v", err, raw)
	}
	if operation.State != domain.StateVerifying {
		t.Fatalf("startup state=%s", operation.State)
	}
	if err := manager.Repository.MarkRestoredUntrusted(context.Background()); err != nil {
		t.Fatal(err)
	}
	operation, err = manager.Repository.ByID(context.Background(), operation.OperationID)
	if err != nil {
		var raw model.DeploymentOperation
		_ = db.Where("operation_id = ?", "deployment-operation:restart").Take(&raw).Error
		t.Fatalf("%v raw=%#v", err, raw)
	}
	if operation.State != domain.StateManualRecoveryRequired || operation.CheckpointRef != "" || !operation.RestoredUntrusted {
		t.Fatalf("restored operation=%#v", operation)
	}
	recovery, err := manager.Repository.Recovery(context.Background())
	if err != nil || recovery.OperationID != operation.OperationID || recovery.State != domain.StateManualRecoveryRequired {
		t.Fatalf("recovery operation=%#v err=%v", recovery, err)
	}
}

func TestDeploymentDoctorRetentionIsBounded(t *testing.T) {
	manager, _, db := deploymentFixture(t)
	for index := 0; index < maxDoctorSnapshots+5; index++ {
		report := domain.FinalizeDoctor(domain.DoctorReport{GeneratedAt: int64(index + 1)})
		if err := manager.Repository.SaveDoctor(context.Background(), report); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.DeploymentDoctorSnapshot{}).Count(&count).Error; err != nil || count != maxDoctorSnapshots {
		t.Fatalf("doctor snapshots=%d err=%v", count, err)
	}
}

func BenchmarkDeploymentIdempotentFakeWorkflowReplay(b *testing.B) {
	manager, _, _ := deploymentFixture(b)
	ctx := context.Background()
	preview, err := manager.Preview(ctx, domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		b.Fatalf("preview=%#v err=%v", preview, err)
	}
	request := StartRequest{TargetProfile: domain.NativeHardened, IdempotencyKey: "deployment-benchmark-replay",
		ExpectedPreviewRevision: preview.Revision, ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true}
	original, err := manager.Start(ctx, request)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		replay, err := manager.Start(ctx, request)
		if err != nil || replay.OperationID != original.OperationID || replay.Revision != original.Revision {
			b.Fatalf("replay=%#v err=%v", replay, err)
		}
	}
}

func deploymentFixture(t testing.TB) (*Manager, *fakeProvider, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DeploymentState{}, &model.DeploymentOperation{}, &model.DeploymentJournal{}, &model.DeploymentDoctorSnapshot{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	provider := &fakeProvider{current: domain.NativeLegacyRoot, now: now}
	manager := NewManager(NewRepository(db), provider)
	manager.Now = func() time.Time { return now }
	manager.Random = strings.NewReader(strings.Repeat("r", 64))
	manager.Management = func(context.Context, time.Time) ManagementPreservation { return readyManagement() }
	manager.Health = func(context.Context, time.Time) RuntimeHealth { return readyRuntimeHealth() }
	return manager, provider, db
}

func readyManagement() ManagementPreservation {
	result := ManagementPreservation{Ready: true, EvidenceRevision: domain.Revision("management-evidence")}
	result.Revision = domain.Revision(result)
	return result
}

func readyRuntimeHealth() RuntimeHealth {
	result := RuntimeHealth{Ready: true, EvidenceRevision: domain.Revision("runtime-health-evidence")}
	result.Revision = domain.Revision(result)
	return result
}

func containsReason(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
