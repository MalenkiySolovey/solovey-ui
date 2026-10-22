package managementcontract

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	ops "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func currentEvidence(e wire.HealthEvidence) *health.Evidence {
	result := &health.Evidence{HostRef: hostref.Ref{Kind: e.Resource.Host.Kind, ID: e.Resource.Host.ID}, Owner: e.Resource.Owner, ResourceID: e.Resource.ResourceID,
		ProofKind: health.ProofKind(e.ProofKind), Source: e.Source, ObservedRevision: string(e.ObservedRevision), EvidenceRevision: string(e.EvidenceRevision),
		ObservedAt: e.ObservedAt, MaxAgeSeconds: e.MaxAgeSeconds, EvidenceRef: e.EvidenceRef}
	if e.DesiredRevision != nil {
		result.DesiredRevision = string(*e.DesiredRevision)
	}
	return result
}

func TestSharedStateVectorsThroughActualOwnerProjections(t *testing.T) {
	counts := map[string]int{}
	for _, raw := range vectorCases(t) {
		var v struct {
			ID, Kind                                              string
			Installed, Enabled, Available                         bool
			Lifecycle, Health                                     string
			SourceStatus, FactCode, ExpectedStatus                string
			Expected                                              json.RawMessage
			Evidence                                              wire.HealthEvidence
			Now                                                   int64
			ExpectedRevision                                      wire.Revision
			ExpectedGeneration, Generation, ResultGeneration      wire.Generation
			Valid, Accepted, Critical                             bool
			Known                                                 *bool
			Reference                                             wire.OperationRef
			Initial, ExpectedAfterRequest, ExpectedAfterSafePoint string
			Artifact                                              wire.ArtifactIdentity
			Status, Value, Revision, Phase, OwnerPhase            string
			Outcome                                               wire.Outcome
			RollbackVerified, RecoveryRequired                    bool
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		counts[v.Kind]++
		t.Run(v.ID, func(t *testing.T) {
			switch v.Kind {
			case "document", "digest", "reason", "command", "removal":
				// Executed by the pinned codec, Action/transition suite and actual
				// supervisor removal suite respectively.
			case "component":
				m := manifest.Manifest{ID: "test-owner", Version: "1.0.0", Actions: []string{}}
				component := state.Component{ID: m.ID, Installed: v.Installed, Enabled: v.Enabled, Active: v.Enabled, AvailableInBinary: v.Available,
					ObservedLifecycle: state.LifecycleObservation{Phase: v.Lifecycle, InstanceRetention: state.InstanceUnknown}}
				var check *health.Result
				if v.Health != "" {
					status := health.StatusOK
					if v.Health == "FAIL" {
						status = health.StatusDegraded
					}
					check = &health.Result{ResourceID: v.Evidence.Resource.ResourceID, Status: status, FactCode: "fixture_result", Evidence: currentEvidence(v.Evidence)}
				}
				at := v.Now
				if at == 0 {
					at = v.Evidence.ObservedAt
				}
				if at == 0 {
					at = 1700000000
				}
				requirement := health.Requirement{Owner: m.ID, Source: "fixture", ProofKind: health.ProbeOK, DesiredRevision: strings.Repeat("a", 64), MaxAge: time.Minute}
				projected, err := ProjectComponent(m, component, check, requirement, "resource:fixture", time.Unix(at, 0))
				var expected string
				if err := json.Unmarshal(v.Expected, &expected); err != nil {
					t.Fatal(err)
				}
				if err != nil || projected.State != expected {
					t.Fatalf("component=%+v expected=%s err=%v", projected, expected, err)
				}
			case "health":
				requirement := health.Requirement{Owner: "test-owner", Source: "fixture", ProofKind: health.ProbeOK, DesiredRevision: string(v.ExpectedRevision), MaxAge: time.Minute}
				status := health.StatusOK
				if v.SourceStatus != "" {
					status = health.Status(v.SourceStatus)
				}
				result := health.Result{ResourceID: "resource:fixture", Status: status, FactCode: v.FactCode, Evidence: currentEvidence(v.Evidence)}
				projected, err := ProjectHealth(result, requirement, time.Unix(v.Now, 0))
				valid := err == nil && projected.Status == "PASS"
				if v.ExpectedGeneration != "" {
					valid = valid && v.Evidence.RuntimeGeneration != nil && *v.Evidence.RuntimeGeneration == v.ExpectedGeneration
					if !valid {
						projected.Status = "UNKNOWN"
						projected.Reason = "STALE_STATE"
					}
				}
				if valid != v.Valid {
					t.Fatalf("health=%+v valid=%v expected=%v err=%v", projected, valid, v.Valid, err)
				}
				if projected.Evidence != nil && projected.Evidence.ObservedAt != v.Evidence.ObservedAt {
					t.Fatal("receipt refreshed evidence")
				}
				if v.ExpectedStatus != "" && projected.Status != v.ExpectedStatus {
					t.Fatalf("status=%s expected=%s", projected.Status, v.ExpectedStatus)
				}
			case "job":
				// Cooperative cancellation is a conformance fixture, not a new
				// durable cancel API. Actual current Job projections stay unsupported.
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				change := ops.Change{Reference: ops.NewReference(v.Reference.Owner, v.Reference.ResourceID, v.Reference.OperationID), Revision: 1, Phase: v.Initial, OwnerPhase: v.Initial, Outcome: ops.OutcomePending}
				if v.Initial == "SUCCEEDED" {
					change.Outcome = ops.OutcomeCommitted
				}
				cancel()
				after := change.OwnerPhase
				if ctx.Err() != nil && change.Outcome == ops.OutcomePending {
					after = "CANCEL_REQUESTED"
					if v.Critical {
						after = "DEFERRED_UNTIL_SAFE_POINT"
					}
				}
				if after != v.ExpectedAfterRequest {
					t.Fatal(after)
				}
				if ctx.Err() != nil && change.Outcome == ops.OutcomePending {
					change.Phase, change.OwnerPhase, change.Outcome = "CANCELLED", "CANCELLED", ops.OutcomeFailed
				}
				job, err := ProjectJob(ops.ProjectJob(change, nil, v.Critical))
				if err != nil || job.Phase != v.ExpectedAfterSafePoint || job.CancelSupported || job.CancelRequested != nil {
					t.Fatalf("job=%+v err=%v", job, err)
				}
			case "lateResult":
				old := state.NewLifecyclePublisher()
				old.RecordStart("test-owner", true)
				current := state.NewLifecyclePublisher()
				current.RecordStart("test-owner", false)
				old.RecordStart("test-owner", true)
				_, stillCurrent := old.Snapshot("test-owner")
				observed, currentOK := current.Snapshot("test-owner")
				if stillCurrent != v.Valid || !currentOK || observed.Phase != state.LifecycleStartFailed || v.Generation == v.ResultGeneration {
					t.Fatal("late publisher replaced current lifecycle")
				}
			case "artifact":
				verified, artifact := signedArtifactFixture(t, v.Artifact)
				if v.Status != "VERIFIED" {
					verified = release.Verified{Manifest: verified.Manifest, Canonical: verified.Canonical, Digest: verified.Digest, KeyID: verified.KeyID}
				}
				projected, err := ProjectArtifact(verified, artifact)
				if (err == nil) != v.Accepted {
					t.Fatalf("artifact trust=%s err=%v", v.Status, err)
				}
				if err == nil && (projected.Artifact.ArtifactID != v.Artifact.ArtifactID || projected.Artifact.ContentDigest != v.Artifact.ContentDigest || projected.Artifact.SourceRevision != wire.Digest(verified.Digest)) {
					t.Fatal("signature/content identity changed")
				}
			case "capability":
				known := v.Known == nil || *v.Known
				projected, err := ProjectCapability("test-owner", "graceful-drain", strings.Repeat("a", 64), "fixture", known, resources.CapabilityValue(v.Value))
				if err != nil || (projected.Value == string(resources.CapabilityYes)) != v.Accepted {
					t.Fatalf("capability=%+v err=%v", projected, err)
				}
			case "change":
				change := ops.Change{Reference: ops.NewReference(v.Reference.Owner, v.Reference.ResourceID, v.Reference.OperationID), Revision: 17, Phase: v.Phase, OwnerPhase: v.OwnerPhase, Outcome: ops.Outcome(v.Outcome), RollbackVerified: v.RollbackVerified, RecoveryRequired: v.RecoveryRequired}
				if v.RecoveryRequired {
					change = change.WithProblem("owner_recovery_required")
				}
				projected, err := ProjectChange(change)
				if err != nil || projected.Outcome != v.Outcome || projected.Revision != wire.Generation(v.Revision) || projected.RollbackVerified != v.RollbackVerified || projected.RecoveryRequired != v.RecoveryRequired {
					t.Fatalf("change=%+v err=%v", projected, err)
				}
				job, err := ProjectJob(ops.ProjectJob(change, &ops.Progress{Completed: 10, Total: 20, Unit: "bytes"}, v.Outcome == "PENDING"))
				if err != nil || job.Reference != projected.Reference || job.Outcome != projected.Outcome || job.Progress == nil || job.CancelSupported || job.CancelRequested != nil {
					t.Fatalf("job/change parity: %+v %v", job, err)
				}
				if v.Outcome != "PENDING" && job.ResultRef == nil {
					t.Fatal("terminal job lost result reference")
				}
			default:
				t.Fatalf("unexecuted shared vector kind %q", v.Kind)
			}
		})
	}
	for _, kind := range []string{"document", "digest", "reason", "command", "component", "health", "job", "lateResult", "artifact", "capability", "change", "removal"} {
		if counts[kind] == 0 {
			t.Fatalf("missing kind %s", kind)
		}
	}
}

func signedArtifactFixture(t *testing.T, input wire.ArtifactIdentity) (release.Verified, release.Artifact) {
	t.Helper()
	now := time.Unix(1700000000, 0)
	artifact := release.Artifact{Name: "solovey-ui-linux-amd64.tar.gz", Role: input.Kind, Platform: input.Platform, Arch: input.Arch, MediaType: "application/gzip", Size: input.Size, SHA256: string(input.ContentDigest), Provenance: input.ProvenanceRef}
	digest := strings.Repeat("b", 64)
	m := release.Manifest{Schema: release.SchemaV1, ReleaseID: "r6-fixture", Sequence: 1, Version: input.Version, Channel: release.ChannelMain, IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
		DeploymentRevision: digest, MinimumPanelVersion: "2026.2.0", MaximumPanelVersion: input.Version, MinimumCoreSchema: "1.13", MaximumCoreSchema: "1.13", TargetCoreSchema: "1.13", BrokerCapability: "broker-capabilities-1.3",
		MigrationSetDigest: digest, ReleaseNotesDigest: digest, RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic",
		Artifacts: []release.Artifact{artifact, {Name: "solovey-ui-core-linux-amd64.tar.gz", Role: "panel-core", Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 90, SHA256: digest, Provenance: "fixture"},
			{Name: "solovey-ui-components.tar.gz", Role: "component-catalog", Platform: "any", Arch: "any", MediaType: "application/gzip", Size: 80, SHA256: digest, Provenance: "fixture"}},
		Components: []release.Component{{ID: "test-owner", Version: "1.0.0", ArtifactSHA256: digest, MinimumCoreSchema: "1.13", MaximumCoreSchema: "1.13"}}}
	seed := sha256.Sum256([]byte("R6 deterministic fixture only; no production key"))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	raw, err := release.Sign(m, "fixture", private)
	if err != nil {
		t.Fatal(err)
	}
	store, err := release.NewTrustStore([]release.TrustRoot{{KeyID: "fixture", PublicKey: public, State: release.RootActive, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := release.Verify(raw, store, now, release.ChannelMain, 0)
	if err != nil {
		t.Fatal(err)
	}
	return verified, artifact
}
