package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	testOperationOne      = "operation-00000000000000000000000000000001"
	testOperationTwo      = "operation-00000000000000000000000000000002"
	testOperationThree    = "operation-00000000000000000000000000000003"
	testOperationMetadata = "operation-00000000000000000000000000000004"
	testOperationValid    = "operation-00000000000000000000000000000005"
	testOperationActive   = "operation-00000000000000000000000000000006"
	testOperationFailed   = "operation-00000000000000000000000000000007"
	testOperationTerminal = "operation-00000000000000000000000000000008"
)

func TestStorageAcceptsDeploymentSuppliedRuntimeShape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "run", "solovey-ui", "server-protection")
	storage, err := New(root)
	if err != nil {
		t.Fatalf("deployment-supplied runtime root was rejected: %v", err)
	}
	if storage.Root() != filepath.Clean(root) {
		t.Fatalf("storage root = %q, want %q", storage.Root(), filepath.Clean(root))
	}
}

func TestInstalledStoragePublishesExactDeploymentProjection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "run", "solovey-ui", "server-protection")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewInstalledWithRecoveryProjection(root, RecoveryProjection{
		Authority: "authenticated_broker", SelfRecoveryAvailable: true,
		Action: RecoveryAction{Program: "ubus", Args: []string{"call", "service", "list", `{"name":"solovey-ui","instance":"root-broker"}`}, Purpose: "verify_privileged_broker_instance"},
	}); err == nil {
		t.Fatal("installed storage accepted recovery guidance without its deployment generation")
	}
	projection := RecoveryProjection{
		Authority: "authenticated_broker", SelfRecoveryAvailable: true,
		Action:            RecoveryAction{Program: "ubus", Args: []string{"call", "service", "list", `{"name":"solovey-ui","instance":"root-broker"}`}, Purpose: "verify_privileged_broker_instance"},
		DeploymentBackend: "procd", ProjectionRevision: strings.Repeat("a", 64), OwnerContractRevision: strings.Repeat("b", 64),
	}
	storage, err := NewInstalledWithRecoveryProjection(root, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateEmergencyRecoveryBundle(RecoveryInput{
		OperationID: testOperationOne, Revision: "installed-projection", State: "recovery_required", ResourceKind: "firewall", CreatedAt: 1, UpdatedAt: 2,
	}, "projection_audit"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(storage.Root(), "recovery", testOperationOne, "recovery-actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document recoveryActionDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.DeploymentBackend != projection.DeploymentBackend || document.ProjectionRevision != projection.ProjectionRevision ||
		document.OwnerContractRevision != projection.OwnerContractRevision || len(document.Actions) == 0 || document.Actions[0].Program != "ubus" {
		t.Fatalf("installed recovery projection document = %#v", document)
	}
}

func TestRecoveryActionsUseDeploymentProjection(t *testing.T) {
	for name, test := range map[string]struct {
		projection RecoveryProjection
		program    string
		forbidden  string
	}{
		"systemd": {
			projection: RecoveryProjection{Authority: "authenticated_broker", SelfRecoveryAvailable: true, Action: RecoveryAction{Program: "systemctl", Args: []string{"is-active", "solovey-privileged-broker.socket"}, Purpose: "verify_privileged_broker_socket"}},
			program:    "systemctl",
		},
		"openwrt": {
			projection: RecoveryProjection{Authority: "authenticated_broker", SelfRecoveryAvailable: true, Action: RecoveryAction{Program: "ubus", Args: []string{"call", "service", "list", `{"name":"solovey-ui","instance":"root-broker"}`}, Purpose: "verify_privileged_broker_instance"}},
			program:    "ubus",
			forbidden:  "systemctl",
		},
	} {
		t.Run(name, func(t *testing.T) {
			storage, err := newStorage(filepath.Join(t.TempDir(), ".runtime", "server-protection"), time.Now, test.projection)
			if err != nil {
				t.Fatal(err)
			}
			actions, err := storage.recoveryActions("firewall")
			if err != nil || len(actions) != 2 || actions[0].Program != test.program {
				t.Fatalf("recovery actions = %#v, %v", actions, err)
			}
			encoded, err := json.Marshal(actions)
			if err != nil {
				t.Fatal(err)
			}
			if test.forbidden != "" && strings.Contains(string(encoded), test.forbidden) {
				t.Fatalf("%s recovery actions contain %q: %s", name, test.forbidden, encoded)
			}
		})
	}
}

func TestOperatorManagedRecoveryBundleFailsClosedWithoutSelfRecovery(t *testing.T) {
	projection := RecoveryProjection{
		Authority:             "external_operator",
		SelfRecoveryAvailable: false,
		Action: RecoveryAction{
			Program: "operator_review",
			Args:    []string{"container_replacement_or_restart"},
			Purpose: "external_operator_managed_recovery",
		},
	}
	storage, err := newStorage(filepath.Join(t.TempDir(), ".runtime", "server-protection"), time.Now, projection)
	if err != nil {
		t.Fatal(err)
	}
	written, err := storage.WriteRevision(testOperationOne, "operator-managed-revision", map[string][]byte{
		"firewall-before.nft": []byte("table inet solovey_protection {}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateRecoveryBundle(RecoveryInput{
		OperationID:  testOperationOne,
		Revision:     "operator-managed-revision",
		State:        "recovery_required",
		ResourceKind: "firewall",
		CreatedAt:    1,
		UpdatedAt:    2,
	}, written.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(storage.Root(), "recovery", testOperationOne, "recovery-actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document recoveryActionDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.RecoveryAuthority != "external_operator" || document.SelfRecoveryAvailable || len(document.Actions) != 2 ||
		document.Actions[0].Program != "operator_review" || document.Actions[0].Purpose != "external_operator_managed_recovery" {
		t.Fatalf("operator-managed recovery document = %#v", document)
	}
	encoded := strings.ToLower(string(data))
	for _, forbidden := range []string{"systemctl", "ubus", "docker restart", "docker compose", "docker.sock", "/var/run/docker"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("operator-managed recovery document contains forbidden control authority %q: %s", forbidden, data)
		}
	}
	for name, malformed := range map[string]RecoveryProjection{
		"external self recovery": {Authority: "external_operator", SelfRecoveryAvailable: true, Action: projection.Action},
		"external broker action": {Authority: "external_operator", SelfRecoveryAvailable: false, Action: RecoveryAction{Program: "systemctl", Args: []string{"restart", "panel"}, Purpose: "external_operator_managed_recovery"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := malformed.Validate(); err == nil {
				t.Fatal("malformed operator-managed recovery projection was accepted")
			}
		})
	}
}

func TestAtomicArtifactWritePublishesManifestLast(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	written, err := storage.WriteRevision(testOperationOne, "revision-one", map[string][]byte{
		"resource-before.json": []byte(`{"owner":"core"}`),
		"firewall-before.nft":  []byte("table inet solovey_protection {}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.ManifestSHA256 == "" || len(written.Manifest.Files) != 2 {
		t.Fatalf("written set = %#v", written)
	}
	if _, err := storage.VerifyRevision("revision-one", written.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	err = filepath.Walk(storage.Root(), func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(info.Name(), ".tmp") {
			t.Fatalf("temporary artifact remained visible: %s", path)
		}
		if runtime.GOOS != "windows" && err == nil && info.IsDir() && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("directory permissions are not restrictive: %s %o", path, info.Mode().Perm())
		}
		if runtime.GOOS != "windows" && err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("file permissions are not restrictive: %s %o", path, info.Mode().Perm())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtifactServicePersistsManifestMetadata(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	writer := &memoryMetadataWriter{}
	item, err := (Service{Storage: storage, Store: writer}).WriteRevision(context.Background(), testOperationMetadata, "revision-metadata", map[string][]byte{
		"resource-before.json": []byte("safe"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.items) != 1 || item.ID == 0 || writer.items[0].ManifestSHA256 == "" || writer.items[0].RelativePath != "revisions/revision-metadata" {
		t.Fatalf("artifact metadata = %#v returned=%#v", writer.items, item)
	}
}

func TestChecksumMismatchIsRejected(t *testing.T) {
	storage := artifactTestStorage(t, time.Now())
	written, err := storage.WriteRevision(testOperationTwo, "revision-two", map[string][]byte{"resource-before.json": []byte("safe")})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storage.Root(), "revisions", "revision-two", "resource-before.json")
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyRevision("revision-two", written.ManifestSHA256); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("checksum error = %v", err)
	}
}

func TestArtifactPathTraversalIsRejected(t *testing.T) {
	storage := artifactTestStorage(t, time.Now())
	for name, run := range map[string]func() error{
		"operation": func() error {
			_, err := storage.WriteRevision("../operation", "revision", map[string][]byte{"safe.json": {}})
			return err
		},
		"revision": func() error {
			_, err := storage.WriteRevision(testOperationValid, "../revision", map[string][]byte{"safe.json": {}})
			return err
		},
		"file": func() error {
			_, err := storage.WriteRevision(testOperationValid, "revision", map[string][]byte{"../escape.json": {}})
			return err
		},
		"remove": func() error { return storage.Remove("../escape") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrPathForbidden) {
				t.Fatalf("path traversal error = %v", err)
			}
		})
	}
}

func TestRecoveryBundleContainsOnlySafeFacts(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	written, err := storage.WriteRevision(testOperationThree, "revision-three", map[string][]byte{
		"private-key.pem":  []byte("PRIVATE KEY super-secret"),
		"core-before.json": []byte(`{"client_uuid":"123e4567-e89b-12d3-a456-426614174000","admin_path":"/secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = storage.CreateRecoveryBundle(RecoveryInput{
		OperationID: testOperationThree, Revision: "revision-three", State: "rollback_failed",
		ResourceID: "123e4567-e89b-12d3-a456-426614174000", ResourceKind: "inbound", Protocol: "tcp",
		Listen: "127.0.0.1", Port: 443, FromOwner: "https://example.test/admin/secret", ToOwner: "fixture-public-owner",
		CreatedAt: 1, UpdatedAt: 2,
		Health: []HealthCheck{{ID: "panel", Status: "failed", FactCode: "listener_unreachable"}},
	}, written.ManifestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.json", "health.json", "artifacts-manifest.json", "recovery-actions.json"} {
		data, readErr := os.ReadFile(filepath.Join(storage.Root(), "recovery", testOperationThree, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{"super-secret", "private-key", "123e4567-e89b-12d3-a456-426614174000", "/secret", "https://", "client_uuid", "command", "cookie", "subscription"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s leaked forbidden value %q: %s", name, forbidden, data)
			}
		}
	}
}

func TestFrontingRecoveryBundleContainsExactHashesWithoutConfigOrSecrets(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	artifactRevision := "fronting-artifact-000000000000000000000001"
	desired, candidate := strings.Repeat("a", 64), strings.Repeat("b", 64)
	previous, previousSHA := strings.Repeat("c", 64), strings.Repeat("d", 64)
	written, err := storage.WriteRevision(testOperationThree, artifactRevision, map[string][]byte{
		"candidate.conf": []byte("proxy_pass 127.0.0.1:9443; # token=must-not-leak\n"),
		"rollback.json":  []byte(`{"previous":"managed"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = storage.CreateRecoveryBundle(RecoveryInput{
		OperationID: testOperationThree, Revision: artifactRevision, State: "rollback_failed", ResourceKind: "fronting",
		DesiredRevision: desired, CandidateSHA256: candidate, PreviousRevision: previous, PreviousSHA256: previousSHA,
		BackendReferenceRevisions: []string{strings.Repeat("e", 64)}, SelectorSetRevision: strings.Repeat("f", 64),
		MapRevision: strings.Repeat("1", 64), UpstreamIDSetRevision: strings.Repeat("2", 64),
		TargetAuthorities:      []TargetAuthorityRecoveryInput{{Kind: "endpoint_lease", ID: "lease-secret-id", Revision: "opaque-provider-revision", State: "active"}},
		ArtifactManifestSHA256: written.ManifestSHA256, CreatedAt: 1, UpdatedAt: 2,
	}, written.ManifestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.json", "health.json", "artifacts-manifest.json", "recovery-actions.json"} {
		data, readErr := os.ReadFile(filepath.Join(storage.Root(), "recovery", testOperationThree, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		text := string(data)
		for _, forbidden := range []string{"proxy_pass", "127.0.0.1:9443", "must-not-leak", "token="} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s leaked %q: %s", name, forbidden, text)
			}
		}
		if name == "summary.json" || name == "artifacts-manifest.json" {
			for field, value := range map[string]string{
				"artifactRevision": artifactRevision, "desiredRevision": desired, "candidateSha256": candidate,
				"previousRevision": previous, "previousSha256": previousSHA, "artifactManifestSha256": written.ManifestSHA256,
			} {
				if !strings.Contains(text, `"`+field+`": "`+value+`"`) {
					t.Fatalf("%s lacks exact %s: %s", name, field, text)
				}
			}
			if name == "summary.json" {
				for _, required := range []string{"targetReferenceRevisions", "selectorSetRevision", "mapRevision", "upstreamIdSetRevision", "targetAuthorities", "endpoint_lease"} {
					if !strings.Contains(text, required) {
						t.Fatalf("%s lacks bounded SNI recovery fact %q: %s", name, required, text)
					}
				}
				for _, forbidden := range []string{"lease-secret-id", "opaque-provider-revision"} {
					if strings.Contains(text, forbidden) {
						t.Fatalf("%s leaked raw authority %q: %s", name, forbidden, text)
					}
				}
			}
		}
	}
}

func TestFrontingOperationRecoveryRejectsCheckpointWithoutExactIdentity(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	artifactRevision := "fronting-artifact-000000000000000000000002"
	canonical := `{"fallback":"boring_close","routes":[]}`
	candidate := "safe generated candidate\n"
	desired, candidateSHA := digestArtifact([]byte(canonical)), digestArtifact([]byte(candidate))
	previous, previousSHA := strings.Repeat("c", 64), strings.Repeat("d", 64)
	written, err := storage.WriteRevision(testOperationThree, artifactRevision, map[string][]byte{
		"candidate.conf": []byte(candidate),
		"canonical.json": []byte(canonical + "\n"),
		"rollback.json":  []byte(`{"revision":"` + previous + `","sha256":"` + previousSHA + `","listeners":[{"address":"0.0.0.0","port":443}]}` + "\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := protectionrepository.ArtifactModel{
		OperationID: testOperationThree, Revision: artifactRevision, Scope: "fronting", RelativePath: written.RelativePath,
		ManifestSHA256: written.ManifestSHA256, Bytes: written.Bytes, CreatedAt: 1, UpdatedAt: 1,
	}
	repository := &memoryOperationRecoveryRepository{artifact: artifact}
	if err := storage.WriteFrontingState(testOperationThree, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	operation := protectionrepository.OperationLockModel{OperationID: testOperationThree, Kind: "fronting", State: "rolling_back"}
	err = (OperationRecovery{Storage: storage, Repository: repository}).CreateBundle(context.Background(), operation, "rollback_failed")
	if err == nil || !strings.Contains(err.Error(), "checkpoint identity is invalid") {
		t.Fatalf("incomplete fronting checkpoint was accepted: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(storage.Root(), "recovery", testOperationThree, "summary.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unsafe recovery bundle was published: %v", statErr)
	}
	wrong := frontingRecoveryEvidence{Version: 1, OperationID: testOperationThree, ArtifactRevision: artifactRevision, DesiredRevision: desired, CandidateSHA256: strings.Repeat("a", 64), PreviousRevision: previous, PreviousSHA256: previousSHA}
	data, _ := json.Marshal(wrong)
	if err := storage.WriteFrontingState(testOperationThree, append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := (OperationRecovery{Storage: storage, Repository: repository}).CreateBundle(context.Background(), operation, "rollback_failed"); err == nil || !strings.Contains(err.Error(), "candidate identity does not match artifact") {
		t.Fatalf("valid-shaped but wrong fronting checkpoint was accepted: %v", err)
	}
	exact := wrong
	exact.CandidateSHA256 = candidateSHA
	data, _ = json.Marshal(exact)
	if err := storage.WriteFrontingState(testOperationThree, append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := (OperationRecovery{Storage: storage, Repository: repository}).CreateBundle(context.Background(), operation, "rollback_failed"); err != nil {
		t.Fatalf("exact fronting recovery evidence was rejected: %v", err)
	}
}

func TestFirewallOperationRecoveryRequiresExactRollbackEvidence(t *testing.T) {
	storage := artifactTestStorage(t, time.Unix(1_700_000_000, 0))
	artifactRevision := "firewall-artifact-000000000000000000000002"
	planRevision := strings.Repeat("a", 64)
	previousRevision := strings.Repeat("b", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + planRevision + "\"\n}\n")
	rollback := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n}\n")
	written, err := storage.WriteRevision(testOperationThree, artifactRevision, map[string][]byte{
		"candidate.nft":      candidate,
		"candidate.sha256":   []byte(digestArtifact(candidate) + "\n"),
		"managed-table.json": []byte(`{"family":"inet","table":"solovey_protection","plan_revision":"` + planRevision + `"}` + "\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rollbackPath := filepath.Join(storage.Root(), "revisions", artifactRevision, "firewall-before.nft")
	if err := os.WriteFile(rollbackPath, rollback, 0o600); err != nil {
		t.Fatal(err)
	}
	rollbackSHA := digestArtifact(rollback)
	if err := os.WriteFile(rollbackPath+".sha256", []byte(rollbackSHA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := protectionrepository.ArtifactModel{OperationID: testOperationThree, Revision: artifactRevision, Scope: "firewall", RelativePath: written.RelativePath, ManifestSHA256: written.ManifestSHA256, Bytes: written.Bytes, CreatedAt: 1, UpdatedAt: 1}
	repository := &memoryOperationRecoveryRepository{artifact: artifact}
	operation := protectionrepository.OperationLockModel{OperationID: testOperationThree, Kind: "firewall", State: "rolling_back", PlanRevision: planRevision}
	wrong := firewallRecoveryEvidence{Version: 1, OperationID: testOperationThree, ArtifactRevision: artifactRevision, PlanRevision: planRevision, CandidateSHA256: digestArtifact(candidate), RollbackSHA256: strings.Repeat("c", 64), PreviousRevision: previousRevision, PreviousTablePresent: true}
	data, _ := json.Marshal(wrong)
	if err := storage.WriteFirewallState(testOperationThree, append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := (OperationRecovery{Storage: storage, Repository: repository}).CreateBundle(context.Background(), operation, "rollback_failed"); err == nil || !strings.Contains(err.Error(), "does not match exact artifacts") {
		t.Fatalf("wrong firewall rollback evidence was accepted: %v", err)
	}
	exact := wrong
	exact.RollbackSHA256 = rollbackSHA
	data, _ = json.Marshal(exact)
	if err := storage.WriteFirewallState(testOperationThree, append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := (OperationRecovery{Storage: storage, Repository: repository}).CreateBundle(context.Background(), operation, "rollback_failed"); err != nil {
		t.Fatalf("exact firewall recovery evidence was rejected: %v", err)
	}
	summary, err := os.ReadFile(filepath.Join(storage.Root(), "recovery", testOperationThree, "summary.json"))
	if err != nil || !strings.Contains(string(summary), `"rollbackSha256": "`+rollbackSHA+`"`) || !strings.Contains(string(summary), `"previousRevision": "`+previousRevision+`"`) {
		t.Fatalf("firewall recovery summary lacks exact evidence: %v\n%s", err, summary)
	}
}

func TestActiveArtifactNeverPrunedAndRollbackFailedBundlePreserved(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	storage := artifactTestStorage(t, now.Add(-100*24*time.Hour))
	store := &memoryMetadataStore{protected: map[string]string{testOperationActive: "applying", testOperationFailed: "rollback_failed"}}
	for index, operation := range []string{testOperationActive, testOperationFailed, testOperationTerminal} {
		revision := operation + "-revision"
		written, err := storage.WriteRevision(operation, revision, map[string][]byte{"resource-before.json": []byte("safe")})
		if err != nil {
			t.Fatal(err)
		}
		store.items = append(store.items, protectionrepository.ArtifactModel{ID: uint(index + 1), OperationID: operation, Revision: revision, RelativePath: written.RelativePath, Bytes: written.Bytes, CreatedAt: now.Add(-100 * 24 * time.Hour).Unix()})
		if operation == testOperationFailed {
			if _, err := storage.CreateRecoveryBundle(RecoveryInput{OperationID: operation, Revision: revision, State: "rollback_failed", CreatedAt: 1, UpdatedAt: 2}, written.ManifestSHA256); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := NewPruner(storage, store, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFilesets != 1 || len(store.items) != 2 {
		t.Fatalf("prune result=%#v remaining=%#v", result, store.items)
	}
	for _, item := range store.items {
		if item.OperationID == testOperationTerminal {
			t.Fatal("terminal artifact outside limits was preserved")
		}
	}
	if _, err := os.Stat(filepath.Join(storage.Root(), "recovery", testOperationFailed, "summary.json")); err != nil {
		t.Fatalf("rollback_failed recovery bundle was pruned: %v", err)
	}
}

func TestArtifactRetentionKeepsCountOrDaysWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	storage := artifactTestStorage(t, now)
	store := &memoryMetadataStore{protected: map[string]string{}}
	ages := []time.Duration{40 * 24 * time.Hour, 35 * 24 * time.Hour, 10 * 24 * time.Hour, 2 * 24 * time.Hour}
	for index, age := range ages {
		operation := fmt.Sprintf("operation-%032x", index+20)
		revision := "revision-retention-" + string(rune('a'+index))
		written, err := storage.WriteRevision(operation, revision, map[string][]byte{"resource-before.json": []byte("safe")})
		if err != nil {
			t.Fatal(err)
		}
		store.items = append(store.items, protectionrepository.ArtifactModel{ID: uint(index + 1), OperationID: operation, Revision: revision, RelativePath: written.RelativePath, Bytes: written.Bytes, CreatedAt: now.Add(-age).Unix()})
	}
	sort.Slice(store.items, func(i, j int) bool { return store.items[i].CreatedAt > store.items[j].CreatedAt })
	result, err := NewPruner(storage, store, func() time.Time { return now }).Prune(context.Background(), 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFilesets != 2 || len(store.items) != 2 {
		t.Fatalf("retention result=%#v remaining=%#v", result, store.items)
	}
}

func TestArtifactsRevisionPublicationFaultMatrixLeavesNoInvisibleDebt(t *testing.T) {
	steps := []string{
		"directory_create",
		"publication_owner_write", "publication_owner_sync", "publication_owner_rename", "publication_owner_directory_sync", "publication_owner_reopen",
		"file_write", "file_sync", "file_rename", "file_directory_sync", "file_reopen",
		"subsequent_file_write", "subsequent_file_sync", "subsequent_file_rename", "subsequent_file_directory_sync", "subsequent_file_reopen",
		"manifest_write", "manifest_sync", "manifest_rename", "manifest_directory_sync", "manifest_reopen",
		"publication_directory_sync", "revision_rename", "revisions_directory_sync", "revision_reopen",
		"pointer_write", "pointer_sync", "pointer_rename", "pointer_directory_sync", "pointer_reopen",
	}
	for index, step := range steps {
		t.Run(step, func(t *testing.T) {
			now := time.Now().UTC()
			storage := artifactTestStorage(t, now.Add(-time.Hour))
			operationID := fmt.Sprintf("operation-%032x", 0x300+index)
			revision := fmt.Sprintf("artifacts-fault-%02d", index)
			fault := errors.New("injected publication fault")
			storage.publicationFault = func(current string) error {
				if current == step {
					return fault
				}
				return nil
			}
			_, err := storage.WriteRevision(operationID, revision, map[string][]byte{
				"a.json": []byte(`{"a":1}`),
				"b.json": []byte(`{"b":2}`),
			})
			if !errors.Is(err, fault) {
				t.Fatalf("fault %s was not returned: %v", step, err)
			}
			if _, statErr := os.Stat(filepath.Join(storage.Root(), "revisions", revision)); step == "pointer_write" && statErr != nil {
				t.Fatalf("pointer fault did not leave an enumerable complete revision: %v", statErr)
			}
			storage.publicationFault = nil
			if err := filepath.Walk(storage.Root(), func(path string, info os.FileInfo, err error) error {
				if err == nil && strings.HasSuffix(info.Name(), ".tmp") {
					t.Fatalf("temporary file survived %s: %s", step, path)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(storage.Root(), "revisions", "foreign.txt")
			if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
			store := &memoryMetadataStore{protected: map[string]string{}}
			if step == "pointer_write" {
				manifest, verifyErr := storage.VerifyRevision(revision, "")
				if verifyErr != nil {
					t.Fatalf("pointer fault revision verify failed: %v", verifyErr)
				}
				if manifest.CreatedAt <= now.Add(time.Hour).Add(-24*time.Hour).Unix() {
					t.Fatalf("pointer fault revision was not recent enough for grace-period enumeration: manifest=%d", manifest.CreatedAt)
				}
			}
			result, pruneErr := NewPruner(storage, store, func() time.Time { return now.Add(time.Hour) }).Prune(context.Background(), 1, 1)
			if pruneErr != nil {
				t.Fatalf("fault %s left unreclaimable debt: result=%#v err=%v", step, result, pruneErr)
			}
			if _, statErr := os.Stat(filepath.Join(storage.Root(), "revisions", revision)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("fault %s left revision debt after sweep: %v", step, statErr)
			}
			if _, statErr := os.Stat(foreign); statErr != nil {
				t.Fatalf("fault %s removed foreign file: %v", step, statErr)
			}
		})
	}
}

func TestArtifactsMetadataPublicationFailureCleansPublishedRevision(t *testing.T) {
	storage := artifactTestStorage(t, time.Now().UTC())
	writer := &memoryMetadataWriter{err: errors.New("injected metadata transaction failure")}
	revision := "artifacts-metadata-failure"
	_, err := (Service{Storage: storage, Store: writer}).WriteRevision(context.Background(), testOperationMetadata, revision, map[string][]byte{"state.json": []byte(`{"safe":true}`)})
	if !errors.Is(err, writer.err) {
		t.Fatalf("metadata failure = %v", err)
	}
	for _, path := range []string{
		filepath.Join(storage.Root(), "revisions", revision),
		filepath.Join(storage.Root(), "operations", testOperationMetadata),
	} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("metadata failure left published state %s: %v", path, statErr)
		}
	}
}

func TestArtifactsOwnedOrphanSweepIsBoundedForeignSafeAndRestartIdempotent(t *testing.T) {
	now := time.Now().UTC().Add(2 * time.Hour)
	storage := artifactTestStorage(t, now.Add(-24*time.Hour))
	untrackedOperation := "operation-00000000000000000000000000000351"
	untrackedRevision := "artifacts-untracked-published"
	if _, err := storage.WriteRevision(untrackedOperation, untrackedRevision, map[string][]byte{"state.json": []byte(`{"published":true}`)}); err != nil {
		t.Fatal(err)
	}
	publicationOperation := "operation-00000000000000000000000000000352"
	publicationPath := filepath.Join(storage.Root(), "publication", publicationStageName(publicationOperation, "interrupted"))
	if err := os.MkdirAll(publicationPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicationPath, "first.json"), []byte(`{"partial":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerData, err := marshalPublicationOwner(publicationOperation, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicationPath, publicationOwnerName), ownerData, 0o600); err != nil {
		t.Fatal(err)
	}
	recoveryOperation := "operation-00000000000000000000000000000353"
	recoveryPath := filepath.Join(storage.Root(), "recovery", recoveryOperation)
	if err := os.MkdirAll(recoveryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recoveryPath, "summary.json"), []byte(`{"orphan":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-time.Hour)
	for _, path := range []string{publicationPath, recoveryPath, filepath.Join(storage.Root(), "operations", untrackedOperation)} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	foreignFile := filepath.Join(storage.Root(), "revisions", "foreign.txt")
	if err := os.WriteFile(foreignFile, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignDirectory := filepath.Join(storage.Root(), "revisions", "foreign-revision")
	if err := os.MkdirAll(foreignDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDirectory, "manifest.json"), []byte("not-owned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &memoryMetadataStore{protected: map[string]string{}}
	first, err := NewPruner(storage, store, func() time.Time { return now }).Prune(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.DeletedOrphans < 3 {
		t.Fatalf("owned orphan result = %#v", first)
	}
	for _, path := range []string{filepath.Join(storage.Root(), "revisions", untrackedRevision), publicationPath, recoveryPath, filepath.Join(storage.Root(), "operations", untrackedOperation)} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("owned orphan survived %s: %v", path, statErr)
		}
	}
	for _, path := range []string{foreignFile, foreignDirectory} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("foreign state was removed %s: %v", path, statErr)
		}
	}
	second, err := NewPruner(storage, store, func() time.Time { return now }).Prune(context.Background(), 1, 1)
	if err != nil || second.DeletedFilesets != 0 || second.DeletedOrphans != 0 || second.DeletedOperations != 0 || second.DeletedTransitions != 0 {
		t.Fatalf("restart prune was not idempotent: %#v err=%v", second, err)
	}
}

type memoryMetadataStore struct {
	items     []protectionrepository.ArtifactModel
	protected map[string]string
}

type memoryMetadataWriter struct {
	items []protectionrepository.ArtifactModel
	err   error
}

type memoryOperationRecoveryRepository struct {
	artifact protectionrepository.ArtifactModel
}

func (r *memoryOperationRecoveryRepository) ArtifactByOperation(_ context.Context, operationID string) (protectionrepository.ArtifactModel, error) {
	if r.artifact.OperationID != operationID {
		return protectionrepository.ArtifactModel{}, protectionrepository.ErrRecordNotFound
	}
	return r.artifact, nil
}

func (r *memoryOperationRecoveryRepository) SaveArtifact(_ context.Context, item *protectionrepository.ArtifactModel) error {
	r.artifact = *item
	return nil
}

func (w *memoryMetadataWriter) SaveArtifact(_ context.Context, item *protectionrepository.ArtifactModel) error {
	if w.err != nil {
		return w.err
	}
	item.ID = uint(len(w.items) + 1)
	w.items = append(w.items, *item)
	return nil
}

func (s *memoryMetadataStore) ListArtifacts(context.Context) ([]protectionrepository.ArtifactModel, error) {
	return append([]protectionrepository.ArtifactModel(nil), s.items...), nil
}
func (s *memoryMetadataStore) ProtectedArtifactOperations(context.Context) (map[string]string, error) {
	return s.protected, nil
}
func (s *memoryMetadataStore) DeleteArtifact(_ context.Context, id uint) error {
	for index := range s.items {
		if s.items[index].ID == id {
			s.items = append(s.items[:index], s.items[index+1:]...)
			return nil
		}
	}
	return errors.New("artifact not found")
}

func (s *memoryMetadataStore) PruneFirewallHistory(context.Context, int, int64, int64) (protectionrepository.FirewallHistoryPruneResult, error) {
	return protectionrepository.FirewallHistoryPruneResult{}, nil
}

func artifactTestStorage(t *testing.T, now time.Time) *Storage {
	t.Helper()
	projection := RecoveryProjection{Authority: "authenticated_broker", SelfRecoveryAvailable: true, Action: RecoveryAction{
		Program: "systemctl", Args: []string{"is-active", "solovey-privileged-broker.socket"}, Purpose: "verify_privileged_broker_socket",
	}}
	storage, err := newStorage(filepath.Join(t.TempDir(), ".runtime", "server-protection"), func() time.Time { return now }, projection)
	if err != nil {
		t.Fatal(err)
	}
	return storage
}
