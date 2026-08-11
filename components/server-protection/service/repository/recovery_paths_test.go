package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestRecoveryPathWriterSealPersistsAcrossRestartAndAvoidsFreshnessChurn(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(50_000, 0).UTC()
	row := RecoveryPathModel{RecoveryPathID: "recovery:persistent", Kind: "SSH", EndpointID: "management:ssh:primary", PrincipalID: "principal:" + strings.Repeat("a", 64), SourcePrefix: "198.51.100.10/32", VerificationMethod: "fresh_ssh_login", VerifiedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: []byte(`[]`), SourceRevision: strings.Repeat("b", 64), ConfigurationRevision: strings.Repeat("c", 64)}
	repository := New(db)
	if err := repository.UpsertRecoveryPath(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	rows, _, err := New(db).ListRecoveryPaths(context.Background(), PageQuery{Page: 1, Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].ProducerRevision != RecoveryPathProducerRevision {
		t.Fatalf("sealed recovery record did not survive repository restart: rows=%#v err=%v", rows, err)
	}
	refreshed := row
	refreshed.VerifiedAt = now.Add(time.Minute).Unix()
	refreshed.ExpiresAt = now.Add(16 * time.Minute).Unix()
	if err := New(db).UpsertRecoveryPath(context.Background(), refreshed); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = New(db).ListRecoveryPaths(context.Background(), PageQuery{Page: 1, Limit: 10})
	if rows[0].VerifiedAt != row.VerifiedAt || rows[0].ExpiresAt != row.ExpiresAt {
		t.Fatal("repeated fresh SSH transport churned an already-fresh snapshot binding")
	}
	if err := New(db).InvalidateRecoveryPathsBySourceRevision(context.Background(), "SSH", strings.Repeat("d", 64), "ssh_verifier_revision_changed"); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = New(db).ListRecoveryPaths(context.Background(), PageQuery{Page: 1, Limit: 10})
	if rows[0].VerificationState != "invalidated" {
		t.Fatal("SSH verifier revision change did not invalidate persisted recovery")
	}
	refreshed.SourceRevision = strings.Repeat("d", 64)
	if err := New(db).UpsertRecoveryPath(context.Background(), refreshed); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = New(db).ListRecoveryPaths(context.Background(), PageQuery{Page: 1, Limit: 10})
	if rows[0].VerificationState != "verified" || rows[0].SourceRevision != refreshed.SourceRevision {
		t.Fatal("new verified login did not re-seal invalidated recovery")
	}
}

func TestMigrationQuarantinesLegacyRecoveryEvidenceWithoutCoreDependency(t *testing.T) {
	db := openTestDB(t)
	if err := db.AutoMigrate(&model.SSHRecoveryEvidence{}); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	legacy := RecoveryPathModel{RecoveryPathID: "recovery:legacy", Kind: "SSH", EndpointID: "management:ssh:legacy",
		PrincipalID: "principal:legacy", VerificationMethod: "fresh_ssh_login", VerifiedAt: 100, ExpiresAt: 200,
		IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: []byte(`[]`),
		SourceRevision: digest, ConfigurationRevision: digest, ProducerRevision: digest}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var evidence model.SSHRecoveryEvidence
	if err := db.Where("id = ?", legacy.RecoveryPathID).First(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if evidence.VerificationState != "invalidated" || evidence.OperationBound || evidence.SingleUse || !strings.Contains(string(evidence.ReasonCodesJSON), "legacy_evidence_requires_reverification") {
		t.Fatalf("legacy evidence was not quarantined: %#v", evidence)
	}
}
