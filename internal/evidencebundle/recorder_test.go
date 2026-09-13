package evidencebundle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestZeroFailureCampaignRecordsEveryCanonicalCheckpointFinalizesAndVerifies(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioPackageLifecycle, ScenarioBrokerReadiness, ScenarioListenerSurface,
		ScenarioSSHRecovery, ScenarioFirewallRecovery, ScenarioStorageRestore})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder := armCampaign(t, root, contract)
	recordCheckpointRange(t, recorder, contract.Checkpoints, 0, len(contract.Checkpoints))
	manifest, err := recorder.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Status != CampaignCompleteSuccess || manifest.FailureCount != 0 ||
		manifest.CheckpointCount != len(contract.Checkpoints) || manifest.Completeness != "PASS" {
		t.Fatalf("unexpected successful manifest: %+v", manifest)
	}
	verified, err := Verify(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != CampaignCompleteSuccess || verified.BundleHash != manifest.BundleHash {
		t.Fatalf("verified status=%q hash=%q", verified.Status, verified.BundleHash)
	}
}

func TestIncompleteCampaignCannotBecomeSuccessful(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	recorder := armCampaign(t, filepath.Join(t.TempDir(), "campaign"), contract)
	recordCheckpointRange(t, recorder, contract.Checkpoints, 0, 1)
	manifest, err := recorder.Finalize()
	if err == nil || manifest.Status != CampaignIncomplete || manifest.Completeness != "FAIL" {
		t.Fatalf("incomplete finalization status=%q completeness=%q err=%v", manifest.Status, manifest.Completeness, err)
	}
}

func TestCompleteFailureRequiresSuccessfulPrefixAndBoundedDiagnostics(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder := armCampaign(t, root, contract)
	recordCheckpointRange(t, recorder, contract.Checkpoints, 0, 1)
	now := time.Now().UTC().Truncate(time.Second)
	failed := contract.Checkpoints[1]
	if err := recorder.ActivateCheckpoint(failed.Scenario, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordFailure(failureInput(now)); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Status != CampaignCompleteFailure || manifest.FailureCount != 1 || len(manifest.Records) != len(groups) {
		t.Fatalf("unexpected failure manifest: %+v", manifest)
	}
	checkpointData, err := os.ReadFile(filepath.Join(root, checkpointsDirectory, checkpointFilename(2)))
	if err != nil {
		t.Fatal(err)
	}
	var failureCheckpoint CheckpointRecord
	if err := json.Unmarshal(checkpointData, &failureCheckpoint); err != nil {
		t.Fatal(err)
	}
	wantEvidenceSHA256, err := failureEvidenceDigest(root, 1)
	if err != nil || failureCheckpoint.EvidenceSHA256 != wantEvidenceSHA256 {
		t.Fatalf("failure evidence digest=%q want=%q err=%v", failureCheckpoint.EvidenceSHA256, wantEvidenceSHA256, err)
	}
	if _, err := Verify(root, contract); err != nil {
		t.Fatal(err)
	}
}

func TestFailureWithoutRequiredPrefixIsIncomplete(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	recorder := armCampaign(t, filepath.Join(t.TempDir(), "campaign"), contract)
	now := time.Now().UTC().Truncate(time.Second)
	failed := contract.Checkpoints[2]
	if err := recorder.ActivateCheckpoint(failed.Scenario, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordFailure(failureInput(now)); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Finalize()
	if err == nil || manifest.Status != CampaignIncomplete {
		t.Fatalf("failure without prefix status=%q err=%v", manifest.Status, err)
	}
}

func TestEpochChainSpansRebootAndSysupgradeAndRejectsUnrelatedEpoch(t *testing.T) {
	for _, transition := range []EpochTransition{EpochReboot, EpochSysupgrade} {
		t.Run(string(transition), func(t *testing.T) {
			contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
			root := filepath.Join(t.TempDir(), "controller-campaign")
			recorder, err := Arm(root, contract)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			if _, err := recorder.BeginEpoch(EpochInput{ID: "epoch-a", Transition: EpochInitial,
				BootID: "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb", ControllerTimestamp: now, TargetTimestamp: now.Add(-37 * 24 * time.Hour),
				ClockState: ClockSkewObserved, ClockSkewMS: int64((37 * 24 * time.Hour) / time.Millisecond)}); err != nil {
				t.Fatal(err)
			}
			recordCheckpointRange(t, recorder, contract.Checkpoints, 0, 2)
			seal, err := recorder.CloseEpoch()
			if err != nil {
				t.Fatal(err)
			}
			// Target-local /run state is expected to be absent after either transition.
			if _, err := OpenArmed(filepath.Join(t.TempDir(), "lost-target-run", "evidence-bundle")); err == nil {
				t.Fatal("missing target-local runtime state was treated as an armed campaign")
			}
			recorder, err = OpenArmed(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := recorder.BeginEpoch(EpochInput{ID: "unrelated", Transition: transition,
				BootID: "cccccccc-1111-2222-3333-dddddddddddd", PreviousEpochHash: strings.Repeat("f", 64),
				ControllerTimestamp: now.Add(time.Minute), TargetTimestamp: now.Add(time.Minute), ClockState: ClockUnverified}); err == nil {
				t.Fatal("unrelated continuation epoch was accepted")
			}
			if _, err := recorder.BeginEpoch(EpochInput{ID: "epoch-b", Transition: transition,
				BootID: "cccccccc-1111-2222-3333-dddddddddddd", PreviousEpochHash: seal.EpochHash,
				ControllerTimestamp: now.Add(time.Minute), TargetTimestamp: now.Add(time.Minute), ClockState: ClockUnverified}); err != nil {
				t.Fatal(err)
			}
			recordCheckpointRange(t, recorder, contract.Checkpoints, 2, len(contract.Checkpoints))
			manifest, err := recorder.Finalize()
			if err != nil || manifest.Status != CampaignCompleteSuccess || len(manifest.Epochs) != 2 {
				t.Fatalf("%s continuation status=%q epochs=%d err=%v", transition, manifest.Status, len(manifest.Epochs), err)
			}
			if _, err := Verify(root, contract); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVerifierRejectsCheckpointManifestAndStaleRunMutation(t *testing.T) {
	for _, name := range []string{"record hash", "scenario substitution", "manifest mutation", "stale run record"} {
		t.Run(name, func(t *testing.T) {
			contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
			root := filepath.Join(t.TempDir(), "campaign")
			recorder := armCampaign(t, root, contract)
			recordCheckpointRange(t, recorder, contract.Checkpoints, 0, len(contract.Checkpoints))
			if _, err := recorder.Finalize(); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "record hash":
				path := filepath.Join(root, checkpointsDirectory, checkpointFilename(1))
				data, _ := os.ReadFile(path)
				var record CheckpointRecord
				if err := json.Unmarshal(data, &record); err != nil {
					t.Fatal(err)
				}
				record.RecordHash = strings.Repeat("f", 64)
				data, _ = json.MarshalIndent(record, "", "  ")
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "scenario substitution":
				path := filepath.Join(root, checkpointsDirectory, checkpointFilename(1))
				var record CheckpointRecord
				data, _ := os.ReadFile(path)
				if err := json.Unmarshal(data, &record); err != nil {
					t.Fatal(err)
				}
				record.Scenario = ScenarioSSHRecovery
				record.RecordHash = ""
				canonical, _ := json.Marshal(record)
				record.RecordHash = digest(canonical)
				data, _ = json.MarshalIndent(record, "", "  ")
				if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			case "manifest mutation":
				path := filepath.Join(root, manifestFilename)
				data, _ := os.ReadFile(path)
				var manifest Manifest
				if err := json.Unmarshal(data, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.BundleHash = strings.Repeat("e", 64)
				data, _ = json.MarshalIndent(manifest, "", "  ")
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "stale run record":
				staleContract := campaignContract([]Scenario{ScenarioBrokerReadiness})
				staleContract.RunID = "stale-previous-run"
				staleRoot := filepath.Join(t.TempDir(), "stale")
				stale := armCampaign(t, staleRoot, staleContract)
				recordCheckpointRange(t, stale, staleContract.Checkpoints, 0, 1)
				staleData, _ := os.ReadFile(filepath.Join(staleRoot, checkpointsDirectory, checkpointFilename(1)))
				if err := os.WriteFile(filepath.Join(root, checkpointsDirectory, checkpointFilename(1)), staleData, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Verify(root, contract); err == nil {
				t.Fatal("tampered campaign was accepted")
			}
		})
	}
}

func TestExpectedAuthorityAndDoubleFinalizationAreRejected(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder := armCampaign(t, root, contract)
	recordCheckpointRange(t, recorder, contract.Checkpoints, 0, len(contract.Checkpoints))
	if _, err := recorder.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Finalize(); err == nil {
		t.Fatal("double finalization was accepted")
	}
	mutations := []func(*Contract){
		func(value *Contract) { value.RunID = "wrong-run" },
		func(value *Contract) { value.ArtifactIdentity = "wrong-artifact.apk" },
		func(value *Contract) {
			value.APKSHA256 = strings.Repeat("a", 64)
			value.ArtifactSHA256 = value.APKSHA256
		},
		func(value *Contract) {
			value.Target.IdentityHash = strings.Repeat("b", 64)
			value.Target.MachineIDHash = value.Target.IdentityHash
		},
		func(value *Contract) {
			value.SourceIdentity = strings.Repeat("c", 64)
			value.SourceFingerprint = value.SourceIdentity
		},
		func(value *Contract) { value.OperatorSHA256 = strings.Repeat("d", 64) },
		func(value *Contract) { value.Target.OpenWrtRevision = "r99999-unrelated" },
		func(value *Contract) {
			value.Scenarios = []Scenario{ScenarioPackageLifecycle}
			value.Checkpoints = CanonicalCheckpoints(value.Scenarios)
		},
	}
	for _, mutate := range mutations {
		expected := contract
		mutate(&expected)
		if _, err := Verify(root, expected); err == nil {
			t.Fatal("wrong expected authority was accepted")
		}
	}
}

func TestUnarmedAndConcurrentRecordingFailClosed(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder, err := Arm(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	first := contract.Checkpoints[0]
	input := checkpointInput(first, now)
	if err := recorder.RecordCheckpoint(input); err == nil {
		t.Fatal("recording without an active epoch/checkpoint was accepted")
	}
	assertCampaignMutationReleased(t, root)
	if _, err := recorder.BeginEpoch(EpochInput{ID: "epoch-a", Transition: EpochInitial,
		BootID: "11111111-2222-3333-4444-555555555555", ControllerTimestamp: now, TargetTimestamp: now, ClockState: ClockSyncProven}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordCheckpoint(input); err == nil {
		t.Fatal("recording without activation was accepted")
	}
	assertCampaignMutationReleased(t, root)
	if err := recorder.ActivateCheckpoint(first.Scenario, first.ID); err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		caller int
		phase  string
		err    string
	}
	const callers = 8
	var successes atomic.Int32
	var ready sync.WaitGroup
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan attempt, callers)
	ready.Add(callers)
	for caller := range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			opened, openErr := OpenArmed(root)
			ready.Done()
			<-start
			if openErr != nil {
				results <- attempt{caller: caller, phase: "open", err: openErr.Error()}
				return
			}
			recordErr := opened.RecordCheckpoint(input)
			if recordErr == nil {
				successes.Add(1)
			}
			result := attempt{caller: caller, phase: "record"}
			if recordErr != nil {
				result.err = recordErr.Error()
			}
			results <- result
		}()
	}
	ready.Wait()
	close(start)
	group.Wait()
	close(results)
	attempts := make([]attempt, 0, callers)
	for result := range results {
		attempts = append(attempts, result)
	}
	if successes.Load() != 1 {
		t.Fatalf("concurrent checkpoint successes=%d, want 1; attempts=%+v", successes.Load(), attempts)
	}
	assertCampaignMutationReleased(t, root)
	if _, err := os.Lstat(filepath.Join(root, activeFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful concurrent record left active checkpoint state: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, checkpointsDirectory, checkpointFilename(1))); err != nil {
		t.Fatalf("successful concurrent record is not durable: %v", err)
	}
	reopened, err := OpenArmed(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.RecordCheckpoint(input); err == nil || !strings.Contains(err.Error(), "no checkpoint transition is active") {
		t.Fatalf("already-recorded checkpoint was not deterministically rejected: %v", err)
	}
	assertCampaignMutationReleased(t, root)
}

func TestSingleArmedCheckpointRecordsAndReleasesMutationClaim(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder := armCampaign(t, root, contract)
	first := contract.Checkpoints[0]
	if err := recorder.ActivateCheckpoint(first.Scenario, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordCheckpoint(checkpointInput(first, time.Now().UTC().Truncate(time.Second))); err != nil {
		t.Fatal(err)
	}
	assertCampaignMutationReleased(t, root)
}

func TestDifferentCampaignCheckpointsRecordConcurrently(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	type fixture struct {
		root     string
		recorder *Recorder
		spec     CheckpointSpec
	}
	fixtures := make([]fixture, 2)
	for index := range fixtures {
		root := filepath.Join(t.TempDir(), "campaign")
		recorder := armCampaign(t, root, contract)
		spec := contract.Checkpoints[index]
		if err := recorder.ActivateCheckpoint(spec.Scenario, spec.ID); err != nil {
			t.Fatal(err)
		}
		fixtures[index] = fixture{root: root, recorder: recorder, spec: spec}
	}

	start := make(chan struct{})
	results := make(chan error, len(fixtures))
	var group sync.WaitGroup
	for _, item := range fixtures {
		item := item
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- item.recorder.RecordCheckpoint(checkpointInput(item.spec, time.Now().UTC().Truncate(time.Second)))
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("independent valid checkpoint failed: %v", err)
		}
	}
	for _, item := range fixtures {
		assertCampaignMutationReleased(t, item.root)
		ordinal := checkpointIndex(contract, item.spec.Scenario, item.spec.ID) + 1
		if _, err := os.Lstat(filepath.Join(item.root, checkpointsDirectory, checkpointFilename(ordinal))); err != nil {
			t.Fatalf("independent checkpoint is not durable: %v", err)
		}
	}
}

func TestFinalizeConcurrentWithCheckpointRecordFailsClosed(t *testing.T) {
	contract := campaignContract([]Scenario{ScenarioBrokerReadiness})
	root := filepath.Join(t.TempDir(), "campaign")
	recorder := armCampaign(t, root, contract)
	first := contract.Checkpoints[0]
	if err := recorder.ActivateCheckpoint(first.Scenario, first.ID); err != nil {
		t.Fatal(err)
	}
	recording, err := OpenArmed(root)
	if err != nil {
		t.Fatal(err)
	}
	finalizing, err := OpenArmed(root)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	recordResult := make(chan error, 1)
	finalizeResult := make(chan error, 1)
	go func() {
		<-start
		recordResult <- recording.RecordCheckpoint(checkpointInput(first, time.Now().UTC().Truncate(time.Second)))
	}()
	go func() {
		<-start
		_, finalizeErr := finalizing.Finalize()
		finalizeResult <- finalizeErr
	}()
	close(start)
	recordErr, finalizeErr := <-recordResult, <-finalizeResult
	if finalizeErr == nil {
		t.Fatal("incomplete concurrent finalization succeeded")
	}
	manifestPresent := false
	if _, statErr := os.Lstat(filepath.Join(root, manifestFilename)); statErr == nil {
		manifestPresent = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
	if recordErr != nil && !manifestPresent {
		t.Fatalf("concurrent record and finalize both lost without a terminal state: record=%v finalize=%v", recordErr, finalizeErr)
	}
	assertCampaignMutationReleased(t, root)
	if manifestPresent {
		if err := recorder.ActivateCheckpoint(first.Scenario, first.ID); !errors.Is(err, errCampaignFinalized) {
			t.Fatalf("post-finalization mutation was not fenced: %v", err)
		}
		if _, err := Verify(root, contract); err == nil {
			t.Fatal("concurrently finalized incomplete campaign verified successfully")
		}
	}
}

func assertCampaignMutationReleased(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, mutationDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("campaign mutation claim was not released: %v", err)
	}
}

func armCampaign(t *testing.T, root string, contract Contract) *Recorder {
	t.Helper()
	recorder, err := Arm(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := recorder.BeginEpoch(EpochInput{ID: "epoch-a", Transition: EpochInitial,
		BootID: "11111111-2222-3333-4444-555555555555", ControllerTimestamp: now, TargetTimestamp: now,
		ClockState: ClockSyncProven}); err != nil {
		t.Fatal(err)
	}
	return recorder
}

func recordCheckpointRange(t *testing.T, recorder *Recorder, specs []CheckpointSpec, start, end int) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	for _, spec := range specs[start:end] {
		if err := recorder.ActivateCheckpoint(spec.Scenario, spec.ID); err != nil {
			t.Fatal(err)
		}
		if err := recorder.RecordCheckpoint(checkpointInput(spec, now)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
}

func checkpointInput(spec CheckpointSpec, at time.Time) CheckpointInput {
	return CheckpointInput{Scenario: spec.Scenario, Checkpoint: spec.ID, ControllerTimestamp: at,
		TargetTimestamp: at.Add(-time.Second), EvidenceClass: "typed-positive-checkpoint", EvidenceSHA256: strings.Repeat("d", 64)}
}

func campaignContract(selected []Scenario) Contract {
	contract := Contract{RunID: "formal-campaign-20260828", SourceIdentity: strings.Repeat("1", 64), SourceFingerprint: strings.Repeat("1", 64),
		ArtifactIdentity: "solovey-ui-2026.3.1-r1.apk", ArtifactSHA256: strings.Repeat("2", 64), APKSHA256: strings.Repeat("2", 64), OperatorSHA256: strings.Repeat("4", 64),
		Target: TargetIdentity{IdentityHash: strings.Repeat("3", 64), MachineIDHash: strings.Repeat("3", 64), OSRelease: "openwrt-25.12.5", Architecture: "aarch64_generic",
			OpenWrtRelease: "25.12.5", OpenWrtRevision: "r33051-f5dae5ece4", OpenWrtTarget: "rockchip/armv8", OpenWrtSubtarget: "armv8", PackageArchitecture: "aarch64_generic"},
		Scenarios: append([]Scenario(nil), selected...)}
	contract.Checkpoints = CanonicalCheckpoints(contract.Scenarios)
	return contract
}

func campaignCollectors(at time.Time) map[Group]Collector {
	digestValue := strings.Repeat("a", 64)
	values := map[Group]Facts{
		GroupService:              {Service: &ServiceFact{Name: "solovey-ui"}},
		GroupInstance:             {Instance: &InstanceFact{Name: "panel"}},
		GroupPeerPID:              {PeerPID: &PeerPIDFact{PID: 321}},
		GroupPeerCredentials:      {PeerCredentials: &PeerCredentialsFact{PID: 321, UID: 65532, GID: 65532}},
		GroupExecutableIdentity:   {ExecutableIdentity: &ExecutableIdentityFact{Label: "/usr/lib/solovey-ui/solovey-ui", SHA256: digestValue, Device: 1, Inode: 2, Mode: 0o100555, UID: 0, GID: 0, Role: "panel"}},
		GroupProcessCgroup:        {ProcessCgroup: &ProcessCgroupFact{Availability: "available", Policy: "required", Unit: "solovey-ui.service", BoundedProcLines: []string{"0::/services/solovey-ui/panel"}, AuthorityRevision: "cgroup-authority-v1"}},
		GroupSupervisorProcess:    {SupervisorProcess: &SupervisorProcessFact{Supervisor: "procd", Service: "solovey-ui", Instance: "panel", PID: 41, StartIdentity: "100", ManifestClient: "panel", ManifestRevision: digestValue}},
		GroupBootIdentity:         {BootIdentity: &BootIdentityFact{BootID: "11111111-2222-3333-4444-555555555555"}},
		GroupProcessStartIdentity: {ProcessStartIdentity: &ProcessStartIdentityFact{StartIdentity: "12345"}},
		GroupBoundedLogs:          {BoundedLogs: &BoundedLogsFact{Source: "broker", WindowStart: at.Add(-LogWindowBefore), WindowEnd: at.Add(LogWindowAfter), Lines: []LogLine{{Timestamp: at, Class: "denial", Message: "role=panel failure=genuine_bounded_failure"}}}},
	}
	collectors := make(map[Group]Collector, len(values))
	for group, facts := range values {
		facts := facts
		collectors[group] = func(context.Context, Trigger) Capture { return Capture{State: StateCaptured, Facts: facts} }
	}
	return collectors
}

func failureInput(at time.Time) FailureInput {
	trigger := Trigger{FailureClass: "genuine_bounded_failure", Timestamp: at,
		ControllerTimestamp: at.Add(time.Second), TargetTimestamp: at}
	collectors := campaignCollectors(at)
	captures := make([]GroupCapture, 0, len(groups))
	for _, group := range groups {
		captures = append(captures, GroupCapture{Group: group,
			Capture: collectors[group](context.Background(), trigger)})
	}
	return FailureInput{FailureClass: trigger.FailureClass, ControllerTimestamp: trigger.ControllerTimestamp,
		TargetTimestamp: trigger.TargetTimestamp, Captures: captures}
}
