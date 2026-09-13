//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/evidencebundle"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestBrokerDenialCaptureRequiresArmingAndWritesCanonicalProjection(t *testing.T) {
	server := &broker.Server{}
	if err := attachArmedDiagnosticCaptureAt(server, filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatal(err)
	}
	if server.Diagnostic != nil {
		t.Fatal("diagnostic capture became always-on without an arm contract")
	}

	root := filepath.Join(t.TempDir(), "run")
	contract := evidencebundle.Contract{RunID: "broker-run-1", SourceIdentity: strings.Repeat("1", 64), SourceFingerprint: strings.Repeat("1", 64),
		ArtifactIdentity: "solovey-ui-2026.3.1-r1.apk", ArtifactSHA256: strings.Repeat("2", 64), APKSHA256: strings.Repeat("2", 64), OperatorSHA256: strings.Repeat("4", 64),
		Target: evidencebundle.TargetIdentity{IdentityHash: strings.Repeat("3", 64), MachineIDHash: strings.Repeat("3", 64), OSRelease: "openwrt-25.12.5", Architecture: "aarch64_generic",
			OpenWrtRelease: "25.12.5", OpenWrtRevision: "r33051-f5dae5ece4", OpenWrtTarget: "rockchip/armv8", OpenWrtSubtarget: "armv8", PackageArchitecture: "aarch64_generic"},
		Scenarios: []evidencebundle.Scenario{evidencebundle.ScenarioBrokerReadiness}}
	recorder, err := evidencebundle.Arm(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := recorder.BeginEpoch(evidencebundle.EpochInput{ID: "boot-epoch-a", Transition: evidencebundle.EpochInitial,
		BootID: "11111111-2222-3333-4444-555555555555", ControllerTimestamp: now, TargetTimestamp: now,
		ClockState: evidencebundle.ClockSkewObserved}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.ActivateCheckpoint(evidencebundle.ScenarioBrokerReadiness, "broker-composition"); err != nil {
		t.Fatal(err)
	}
	if err := attachArmedDiagnosticCaptureAt(server, root); err != nil {
		t.Fatal(err)
	}
	if server.Diagnostic == nil {
		t.Fatal("armed broker did not attach its bounded diagnostic sink")
	}
	at := time.Now().UTC().Truncate(time.Second)
	server.Diagnostic(broker.DiagnosticEvent{Timestamp: at, PeerRole: broker.RolePanel,
		PeerAttestation: broker.PeerAttestationWriterMismatch, ResultClass: "denied_unauthorized", Phase: broker.AuditPhaseFinalRecheck,
		Peer: broker.PeerIdentity{PID: os.Getpid(), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()),
			Executable: "/usr/lib/solovey-ui/solovey-ui", ExecutableDigest: strings.Repeat("a", 64), Device: 1, Inode: 2,
			ExecutableMode: 0o100555, ExecutableUID: 0, ExecutableGID: 0, StartTime: "100", BootID: "11111111-2222-3333-4444-555555555555",
			CgroupAvailability: "available", CgroupPolicy: broker.CgroupRequired, CgroupUnit: "solovey-ui.service",
			CgroupAuthorityRevision: "cgroup-authority-v1", Supervisor: "procd", SupervisorPID: os.Getppid(), SupervisorStart: "90",
			ProcdService: "solovey-ui", ProcdInstance: "panel", ManifestClient: "panel", ManifestRevision: strings.Repeat("b", 64)}})

	reopened, err := evidencebundle.OpenArmed(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := reopened.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Records) != 10 || manifest.FailureCount != 1 {
		t.Fatalf("broker denial capture records=%d failures=%d", len(manifest.Records), manifest.FailureCount)
	}
	if _, err := evidencebundle.Verify(root, contract); err != nil {
		t.Fatal(err)
	}
}
