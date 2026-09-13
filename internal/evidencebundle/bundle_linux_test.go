//go:build linux

package evidencebundle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArmedBundleCapturesTenBoundedGroupsAndVerifiesIdentity(t *testing.T) {
	contract := testContract()
	root := filepath.Join(t.TempDir(), "run")
	recorder, err := Arm(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
	at := time.Now().UTC().Truncate(time.Second)
	if err := recorder.CaptureFailure(context.Background(), Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "peer_writer_mismatch", Timestamp: at}, completeCollectors(at)); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FailureCount != 1 || len(manifest.Records) != len(Groups()) || manifest.Completeness != "PASS" || manifest.BundleHash == "" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	verified, err := Verify(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	if verified.BundleHash != manifest.BundleHash {
		t.Fatal("verified bundle hash differs")
	}
}

func TestCompletenessStatesAreClosedAndFailureStatesFailTheRun(t *testing.T) {
	for _, item := range []struct {
		name      string
		group     Group
		capture   Capture
		wantError bool
	}{
		{name: "not applicable", group: GroupBoundedLogs, capture: Capture{State: StateNotApplicable, Reason: "owner_has_no_log_source"}},
		{name: "capture error", group: GroupService, capture: Capture{State: StateCaptureError, Reason: "service_identity_unavailable"}, wantError: true},
		{name: "unstable", group: GroupPeerPID, capture: Capture{State: StateUnstable, Reason: "peer_identity_changed"}, wantError: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			contract := testContract()
			recorder, err := Arm(filepath.Join(t.TempDir(), "run"), contract)
			if err != nil {
				t.Fatal(err)
			}
			beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
			at := time.Now().UTC().Truncate(time.Second)
			collectors := completeCollectors(at)
			collectors[item.group] = func(context.Context, Trigger) Capture { return item.capture }
			captureErr := recorder.CaptureFailure(context.Background(), Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "bounded_failure", Timestamp: at}, collectors)
			_, finalErr := recorder.Finalize()
			if item.wantError && (captureErr == nil || finalErr == nil) {
				t.Fatalf("failure state did not fail capture/finalization: capture=%v final=%v", captureErr, finalErr)
			}
			if !item.wantError && (captureErr != nil || finalErr != nil) {
				t.Fatalf("schema-defined NOT_APPLICABLE failed: capture=%v final=%v", captureErr, finalErr)
			}
		})
	}
}

func TestProcessCgroupCaptureFailsClosedAboveByteLimit(t *testing.T) {
	contract := testContract()
	recorder, err := Arm(filepath.Join(t.TempDir(), "run"), contract)
	if err != nil {
		t.Fatal(err)
	}
	beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
	at := time.Now().UTC().Truncate(time.Second)
	collectors := completeCollectors(at)
	collectors[GroupProcessCgroup] = func(context.Context, Trigger) Capture {
		return Capture{State: StateCaptured, Facts: Facts{ProcessCgroup: &ProcessCgroupFact{
			Availability: "required", Policy: "exact", BoundedProcLines: []string{strings.Repeat("x", MaxCgroupBytes)},
		}}}
	}
	if err := recorder.CaptureFailure(context.Background(), Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "oversized_cgroup", Timestamp: at}, collectors); err == nil {
		t.Fatal("oversized cgroup evidence was accepted")
	}
}

func TestVerifierRejectsAuthorityIntegrityCardinalityAndSecretFailures(t *testing.T) {
	t.Run("authority bindings", func(t *testing.T) {
		root, contract := completeBundle(t)
		mutations := []func(*Contract){
			func(value *Contract) { value.RunID = "wrong-run" },
			func(value *Contract) { value.SourceIdentity = strings.Repeat("b", 64) },
			func(value *Contract) { value.ArtifactSHA256 = strings.Repeat("c", 64) },
			func(value *Contract) { value.Target.MachineIDHash = strings.Repeat("d", 64) },
			func(value *Contract) { value.Scenarios = []Scenario{ScenarioSSHRecovery} },
		}
		for index, mutate := range mutations {
			expected := contract
			expected.Scenarios = append([]Scenario(nil), contract.Scenarios...)
			mutate(&expected)
			if _, err := Verify(root, expected); err == nil {
				t.Errorf("authority mutation %d was accepted", index)
			}
		}
	})
	for _, item := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing record", mutate: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, recordsDirectory, "01-service.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unexpected record", mutate: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, recordsDirectory, "01-service-duplicate.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "modified record", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, recordsDirectory, "01-service.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data[:len(data)-2], ' ', '\n')
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unbounded record", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, recordsDirectory, "01-service.json")
			if err := os.WriteFile(path, make([]byte, MaxRecordBytes+1), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			root, contract := completeBundle(t)
			item.mutate(t, root)
			if _, err := Verify(root, contract); err == nil {
				t.Fatal("corrupt bundle was accepted")
			}
		})
	}

	t.Run("secret positive evidence", func(t *testing.T) {
		contract := testContract()
		recorder, err := Arm(filepath.Join(t.TempDir(), "run"), contract)
		if err != nil {
			t.Fatal(err)
		}
		beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
		at := time.Now().UTC().Truncate(time.Second)
		collectors := completeCollectors(at)
		collectors[GroupBoundedLogs] = func(context.Context, Trigger) Capture {
			return Capture{State: StateCaptured, Facts: Facts{BoundedLogs: &BoundedLogsFact{Source: "broker", WindowStart: at.Add(-time.Second), WindowEnd: at.Add(time.Second),
				Lines: []LogLine{{Timestamp: at, Class: "denial", Message: "password=should-never-be-retained"}}}}}
		}
		if err := recorder.CaptureFailure(context.Background(), Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "secret_scan", Timestamp: at}, collectors); err == nil {
			t.Fatal("secret-positive capture did not fail")
		}
		manifest, err := recorder.Finalize()
		if err == nil || manifest.SecretScan != "FAIL" {
			t.Fatalf("secret-positive bundle finalization=%v scan=%s", err, manifest.SecretScan)
		}
		data, err := os.ReadFile(filepath.Join(recorder.root, recordsDirectory, "01-bounded_logs.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "should-never-be-retained") {
			t.Fatal("secret value was retained")
		}
	})
}

func TestDuplicateSingularRecordCannotOverwrite(t *testing.T) {
	contract := testContract()
	recorder, err := Arm(filepath.Join(t.TempDir(), "run"), contract)
	if err != nil {
		t.Fatal(err)
	}
	beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
	at := time.Now().UTC().Truncate(time.Second)
	trigger := Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "duplicate_event", Timestamp: at}
	if err := recorder.CaptureFailure(context.Background(), trigger, completeCollectors(at)); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenArmed(recorder.root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.next = 1
	if err := reopened.CaptureFailure(context.Background(), trigger, completeCollectors(at)); err == nil {
		t.Fatal("duplicate singular records overwrote the first event")
	}
}

func completeBundle(t *testing.T) (string, Contract) {
	t.Helper()
	contract := testContract()
	root := filepath.Join(t.TempDir(), "run")
	recorder, err := Arm(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	beginAndActivate(t, recorder, ScenarioBrokerReadiness, "broker-composition")
	at := time.Now().UTC().Truncate(time.Second)
	if err := recorder.CaptureFailure(context.Background(), Trigger{Scenario: ScenarioBrokerReadiness, FailureClass: "complete", Timestamp: at}, completeCollectors(at)); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Finalize(); err != nil {
		t.Fatal(err)
	}
	return root, contract
}

func testContract() Contract {
	return Contract{RunID: "run-20260826-01", SourceIdentity: strings.Repeat("1", 64), SourceFingerprint: strings.Repeat("1", 64),
		ArtifactIdentity: "solovey-ui-2026.3.1-r1.apk", ArtifactSHA256: strings.Repeat("2", 64), APKSHA256: strings.Repeat("2", 64), OperatorSHA256: strings.Repeat("4", 64),
		Target: TargetIdentity{IdentityHash: strings.Repeat("3", 64), MachineIDHash: strings.Repeat("3", 64), OSRelease: "openwrt-25.12.5", Architecture: "aarch64_generic",
			OpenWrtRelease: "25.12.5", OpenWrtRevision: "r33051-f5dae5ece4", OpenWrtTarget: "rockchip/armv8", OpenWrtSubtarget: "armv8", PackageArchitecture: "aarch64_generic"},
		Scenarios: []Scenario{ScenarioBrokerReadiness}}
}

func beginAndActivate(t *testing.T, recorder *Recorder, scenario Scenario, checkpoint string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := recorder.BeginEpoch(EpochInput{ID: "boot-epoch-a", Transition: EpochInitial,
		BootID: "11111111-2222-3333-4444-555555555555", ControllerTimestamp: now, TargetTimestamp: now,
		ClockState: ClockSkewObserved}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.ActivateCheckpoint(scenario, checkpoint); err != nil {
		t.Fatal(err)
	}
}

func completeCollectors(at time.Time) map[Group]Collector {
	digest := strings.Repeat("a", 64)
	values := map[Group]Facts{
		GroupService:              {Service: &ServiceFact{Name: "solovey-ui"}},
		GroupInstance:             {Instance: &InstanceFact{Name: "panel"}},
		GroupPeerPID:              {PeerPID: &PeerPIDFact{PID: 321}},
		GroupPeerCredentials:      {PeerCredentials: &PeerCredentialsFact{PID: 321, UID: 65532, GID: 65532}},
		GroupExecutableIdentity:   {ExecutableIdentity: &ExecutableIdentityFact{Label: "/usr/lib/solovey-ui/solovey-ui", SHA256: digest, Device: 1, Inode: 2, Mode: 0o100555, UID: 0, GID: 0, Role: "panel"}},
		GroupProcessCgroup:        {ProcessCgroup: &ProcessCgroupFact{Availability: "available", Policy: "required", Unit: "solovey-ui.service", BoundedProcLines: []string{"0::/services/solovey-ui/panel"}, AuthorityRevision: "cgroup-authority-v1"}},
		GroupSupervisorProcess:    {SupervisorProcess: &SupervisorProcessFact{Supervisor: "procd", Service: "solovey-ui", Instance: "panel", PID: 41, StartIdentity: "100", ManifestClient: "panel", ManifestRevision: digest}},
		GroupBootIdentity:         {BootIdentity: &BootIdentityFact{BootID: "11111111-2222-3333-4444-555555555555"}},
		GroupProcessStartIdentity: {ProcessStartIdentity: &ProcessStartIdentityFact{StartIdentity: "12345"}},
		GroupBoundedLogs:          {BoundedLogs: &BoundedLogsFact{Source: "broker", WindowStart: at.Add(-LogWindowBefore), WindowEnd: at.Add(LogWindowAfter), Lines: []LogLine{{Timestamp: at, Class: "denial", Message: "role=panel failure=peer_writer_mismatch"}}}},
	}
	collectors := make(map[Group]Collector, len(values))
	for group, facts := range values {
		facts := facts
		collectors[group] = func(context.Context, Trigger) Capture { return Capture{State: StateCaptured, Facts: facts} }
	}
	return collectors
}

func TestManifestJSONContainsNoUnboundedMapPayload(t *testing.T) {
	root, _ := completeBundle(t)
	data, err := os.ReadFile(filepath.Join(root, manifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest["payload"]; exists {
		t.Fatal("bundle manifest acquired an arbitrary payload")
	}
}

func TestVerifierRejectsSymlinkAndDirectorySubstitution(t *testing.T) {
	t.Run("record symlink", func(t *testing.T) {
		root, contract := completeBundle(t)
		path := filepath.Join(root, recordsDirectory, "01-service.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(root, contract); err == nil {
			t.Fatal("symlinked record was accepted")
		}
	})
	t.Run("records directory symlink", func(t *testing.T) {
		contract := testContract()
		root := filepath.Join(t.TempDir(), "campaign")
		if _, err := Arm(root, contract); err != nil {
			t.Fatal(err)
		}
		records := filepath.Join(root, recordsDirectory)
		if err := os.Remove(records); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, records); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenArmed(root); err == nil {
			t.Fatal("symlinked records directory was accepted")
		}
	})
}
