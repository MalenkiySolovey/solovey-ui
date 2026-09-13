package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/evidencebundle"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestOperatorExposesArmRecordFinalizeAndVerifyBeforeProductInstallation(t *testing.T) {
	temporary := t.TempDir()
	root := filepath.Join(temporary, "campaign")
	contract := operatorContract()
	contractPath := writeFixture(t, temporary, "contract.json", contract)
	var output bytes.Buffer
	if err := run([]string{"arm", root, contractPath}, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("arm emitted no normalized contract")
	}
	now := time.Now().UTC().Truncate(time.Second)
	epochPath := writeFixture(t, temporary, "epoch.json", evidencebundle.EpochInput{ID: "epoch-a", Transition: evidencebundle.EpochInitial,
		BootID: "11111111-2222-3333-4444-555555555555", ControllerTimestamp: now, TargetTimestamp: now.Add(-time.Hour),
		ClockState: evidencebundle.ClockSkewObserved, ClockSkewMS: int64(time.Hour / time.Millisecond)})
	output.Reset()
	if err := run([]string{"begin-epoch", root, epochPath}, &output); err != nil {
		t.Fatal(err)
	}
	for index, checkpoint := range evidencebundle.CanonicalCheckpoints(contract.Scenarios) {
		if err := run([]string{"activate", root, string(checkpoint.Scenario), checkpoint.ID}, &output); err != nil {
			t.Fatal(err)
		}
		input := evidencebundle.CheckpointInput{Scenario: checkpoint.Scenario, Checkpoint: checkpoint.ID,
			ControllerTimestamp: now.Add(time.Duration(index) * time.Second), TargetTimestamp: now.Add(time.Duration(index-1) * time.Second),
			EvidenceClass: "typed-positive-checkpoint", EvidenceSHA256: strings.Repeat("d", 64)}
		inputPath := writeFixture(t, temporary, "record-"+checkpoint.ID+".json", input)
		if err := run([]string{"record", root, inputPath}, &output); err != nil {
			t.Fatal(err)
		}
	}
	output.Reset()
	if err := run([]string{"finalize", root}, &output); err != nil {
		t.Fatal(err)
	}
	var manifest evidencebundle.Manifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != evidencebundle.CampaignCompleteSuccess {
		t.Fatalf("operator final status=%q", manifest.Status)
	}
	output.Reset()
	if err := run([]string{"verify", root, contractPath}, &output); err != nil {
		t.Fatal(err)
	}
}

func TestOperatorRejectsUnknownAndUnboundedInput(t *testing.T) {
	if err := run([]string{"shell", t.TempDir()}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown operator action was accepted")
	}
	path := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(path, make([]byte, maxOperatorInput+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"arm", filepath.Join(t.TempDir(), "campaign"), path}, &bytes.Buffer{}); err == nil {
		t.Fatal("unbounded operator input was accepted")
	}
}

func TestOperatorRecentIsRootPathFixedReadOnlyAndBounded(t *testing.T) {
	document := broker.RecentDiagnostics{Schema: broker.RecentDiagnosticSchema, Records: []broker.RecentDiagnostic{{
		Timestamp: time.Now().UTC().UnixMilli(), Owner: "listener_evidence", Operation: broker.VerbSSHObserve,
		Stage: "socket_table_read", Reason: "socket_table_unavailable", ErrnoClass: "EACCES",
		ProofMethod: "procfs_fd_inode_socket_table/v1", DescriptorCount: 12, SocketDescriptorCount: 2,
	}}}
	called := 0
	var output bytes.Buffer
	if err := runWithRecentReader([]string{"recent"}, &output, func() (broker.RecentDiagnostics, error) {
		called++
		return document, nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("recent reader calls=%d", called)
	}
	var got broker.RecentDiagnostics
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != document.Schema || len(got.Records) != 1 || got.Records[0].Operation != broker.VerbSSHObserve ||
		got.Records[0].Stage != "socket_table_read" || got.Records[0].Reason != "socket_table_unavailable" {
		t.Fatalf("recent operator output = %#v", got)
	}
	if err := runWithRecentReader([]string{"recent", t.TempDir()}, &bytes.Buffer{}, func() (broker.RecentDiagnostics, error) {
		return document, nil
	}); err == nil {
		t.Fatal("recent accepted a caller-selected path")
	}
}

func operatorContract() evidencebundle.Contract {
	return evidencebundle.Contract{RunID: "operator-campaign", SourceIdentity: strings.Repeat("1", 64), SourceFingerprint: strings.Repeat("1", 64),
		ArtifactIdentity: "solovey-ui-2026.3.1-r1.apk", ArtifactSHA256: strings.Repeat("2", 64), APKSHA256: strings.Repeat("2", 64), OperatorSHA256: strings.Repeat("4", 64),
		Target: evidencebundle.TargetIdentity{IdentityHash: strings.Repeat("3", 64), MachineIDHash: strings.Repeat("3", 64), OSRelease: "openwrt-25.12.5", Architecture: "aarch64_generic",
			OpenWrtRelease: "25.12.5", OpenWrtRevision: "r33051-f5dae5ece4", OpenWrtTarget: "rockchip/armv8", OpenWrtSubtarget: "armv8", PackageArchitecture: "aarch64_generic"},
		Scenarios: []evidencebundle.Scenario{evidencebundle.ScenarioBrokerReadiness}}
}

func writeFixture(t *testing.T, directory, name string, value any) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
