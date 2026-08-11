package coreinboundcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type recordingCoordinator struct {
	calls int
}

func (c *recordingCoordinator) RunBlockingContext(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.calls++
	return operation()
}

type fakePatchRuntime struct {
	observation           RuntimeInboundObservationV1
	applyErr              error
	observeErr            error
	applyCalls            int
	cancel                context.CancelFunc
	candidate             func() []byte
	preserveOptionsDigest bool
}

func (r *fakePatchRuntime) ApplyInbound(ctx context.Context, _ uint) (RuntimeInboundObservationV1, error) {
	r.applyCalls++
	if r.candidate != nil && !r.preserveOptionsDigest {
		digest, err := canonicalInboundOptionsDigest(ctx, r.candidate())
		if err != nil {
			return RuntimeInboundObservationV1{}, err
		}
		r.observation.OptionsDigest = digest
	}
	if r.cancel != nil {
		r.cancel()
	}
	return r.observation, r.applyErr
}

func (r *fakePatchRuntime) ObserveInbound(context.Context, string) (RuntimeInboundObservationV1, error) {
	return r.observation, r.observeErr
}

type fakePatchHooks struct {
	beforeCalls int
	afterCalls  int
	failBefore  bool
}

func (h *fakePatchHooks) BeforeCommit(context.Context, *gorm.DB, *model.Inbound, FallbackPatchVariantV1, []ChangedFieldV1) error {
	h.beforeCalls++
	if h.failBefore {
		return errors.New("injected hook failure")
	}
	return nil
}

func (h *fakePatchHooks) AfterCommit(FallbackPatchVariantV1, uint) {
	h.afterCalls++
}

type fakePatchValidator struct {
	calls int
	fail  bool
	last  []byte
}

func (v *fakePatchValidator) ValidateInbound(_ context.Context, content []byte) error {
	v.calls++
	v.last = append(v.last[:0], content...)
	if v.fail {
		return errors.New("injected validation failure")
	}
	return nil
}

type fakeCandidateHydrator struct {
	calls int
}

func (h *fakeCandidateHydrator) HydrateInbound(_ context.Context, _ *gorm.DB, inbound *model.Inbound, content []byte) ([]byte, error) {
	h.calls++
	object, err := decodeObject(content)
	if err != nil {
		return nil, err
	}
	if inbound != nil && inbound.Type == "trojan" {
		object["users"] = json.RawMessage(`[{"name":"candidate-user-secret","password":"candidate-credential-secret"}]`)
	} else {
		object["users"] = json.RawMessage(`[{"name":"candidate-user-secret","uuid":"candidate-credential-secret"}]`)
	}
	return json.Marshal(object)
}

type patchFixture struct {
	db          *gorm.DB
	service     *Service
	coordinator *recordingCoordinator
	runtime     *fakePatchRuntime
	hooks       *fakePatchHooks
	validator   *fakePatchValidator
	hydrator    *fakeCandidateHydrator
	now         time.Time
}

var patchFixtureSerial atomic.Uint64

func newPatchFixture(t *testing.T, inbound model.Inbound) *patchFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()), patchFixtureSerial.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.InboundFallbackCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	if inbound.Tls != nil {
		tlsRecord := *inbound.Tls
		if err = db.Create(&tlsRecord).Error; err != nil {
			t.Fatal(err)
		}
		inbound.Tls = nil
	}
	if err = db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	coordinator := &recordingCoordinator{}
	validator := &fakePatchValidator{}
	runtime := &fakePatchRuntime{observation: RuntimeInboundObservationV1{
		RuntimeAvailable: true, Tag: inbound.Tag, Type: inbound.Type,
		OptionsDigest: strings.Repeat("d", 64), ManagerGeneration: 7, MatchingInboundCount: 1,
	}}
	runtime.candidate = func() []byte { return validator.last }
	hooks := &fakePatchHooks{}
	hydrator := &fakeCandidateHydrator{}
	fixture := &patchFixture{
		db: db, coordinator: coordinator, runtime: runtime, hooks: hooks, validator: validator, hydrator: hydrator,
		now: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
	}
	fixture.service = NewWithMutations(db, nil, MutationDependencies{
		Coordinator: coordinator, Runtime: runtime, Hooks: hooks, Hydrator: hydrator, validator: validator,
		now: func() time.Time { return fixture.now },
	})
	fixture.service.identity = exactIdentity(true)
	return fixture
}

func realityRow() model.Inbound {
	return model.Inbound{
		Id: 31, Type: "vless", Tag: "reality-inbound", TlsId: 11,
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":24443,"tcp_fast_open":true}`),
		Tls: &model.Tls{Id: 11, Name: "reality", Server: json.RawMessage(`{
			"enabled":true,"server_name":"cover.example",
			"reality":{"enabled":true,"handshake":{"server":"127.0.0.1","server_port":18443},"private_key":"private-secret","short_id":["short-secret"]}
		}`)},
	}
}

func trojanRow() model.Inbound {
	return model.Inbound{
		Id: 32, Type: "trojan", Tag: "trojan-inbound", TlsId: 12,
		Options: json.RawMessage(`{
			"listen":"127.0.0.1","listen_port":25443,"tcp_fast_open":true,
			"fallback":{"server":"127.0.0.1","server_port":18080},
			"fallback_for_alpn":{"h2":{"server":"127.0.0.1","server_port":18081},"http/1.1":{"server":"127.0.0.1","server_port":18080}}
		}`),
		Tls: &model.Tls{Id: 12, Name: "trojan", Server: json.RawMessage(`{"enabled":true,"alpn":["http/1.1","h2"],"certificate":["certificate-secret"],"key":["key-secret"]}`)},
	}
}

func tlsEndpoint() ApprovedEndpointV1 {
	return ApprovedEndpointV1{
		ProviderID: "provider-a", EndpointID: "endpoint-a", EndpointRevision: strings.Repeat("a", 64),
		Network: "tcp", AddressFamily: "ipv4", Bind: "127.0.0.1", Port: 19443, Local: true,
		TransportSecurity: "tls", ApplicationProtocols: []string{"http/1.1", "h2"},
	}
}

func plaintextEndpoint() ApprovedEndpointV1 {
	endpoint := tlsEndpoint()
	endpoint.EndpointID = "endpoint-b"
	endpoint.EndpointRevision = strings.Repeat("b", 64)
	endpoint.Port = 19080
	endpoint.TransportSecurity = "none"
	return endpoint
}

func previewRequest(t *testing.T, fixture *patchFixture, variant FallbackPatchVariantV1, endpoint ApprovedEndpointV1, replaceDefault bool) PreviewFallbackPatchRequestV1 {
	t.Helper()
	snapshot, err := fixture.service.Snapshot(t.Context(), fixture.runtimeInboundID())
	if err != nil {
		t.Fatal(err)
	}
	return PreviewFallbackPatchRequestV1{
		Expected: FallbackPatchExpectationsV1{
			InboundDatabaseID: snapshot.InboundDatabaseID, ResourceID: snapshot.ResourceID,
			ConfigurationRevision:      snapshot.ConfigurationRevision,
			RuntimeIdentityRevision:    snapshot.RuntimeIdentityRevision,
			CapabilityResolverRevision: snapshot.CapabilityResolverRevision,
			EndpointRevision:           endpoint.EndpointRevision,
		},
		Variant: variant, ApprovedEndpoint: endpoint, ReplaceDefaultToo: replaceDefault,
	}
}

func prepareCheckpoint(t *testing.T, fixture *patchFixture, preview FallbackPatchPreviewV1, endpoint ApprovedEndpointV1, replaceDefault bool) (CheckpointPreparationV1, error) {
	t.Helper()
	return fixture.service.PrepareCheckpoint(t.Context(), PrepareCheckpointRequestV1{
		Preview: preview, ApprovedEndpoint: endpoint, ReplaceDefaultToo: replaceDefault,
	})
}

func (f *patchFixture) runtimeInboundID() uint {
	var id uint
	_ = f.db.Model(&model.Inbound{}).Select("id").Limit(1).Scan(&id).Error
	return id
}

func TestRealityPatchCheckpointApplyVerifyRestoreAndRelease(t *testing.T) {
	fixture := newPatchFixture(t, realityRow())
	var beforeTLS model.Tls
	if err := fixture.db.First(&beforeTLS, 11).Error; err != nil {
		t.Fatal(err)
	}
	request := previewRequest(t, fixture, FallbackPatchVLESSRealityHandshakeTCP, tlsEndpoint(), false)
	preview, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
	if err != nil || second.Digest != preview.Digest || preview.PreviewID != preview.Digest {
		t.Fatalf("deterministic preview failed: %#v %#v %v", preview, second, err)
	}
	if len(preview.ChangedFields) != 2 || fixture.validator.calls != 2 {
		t.Fatalf("preview facts = %#v, validator calls=%d", preview, fixture.validator.calls)
	}
	if fixture.hydrator.calls < 2 || !strings.Contains(string(fixture.validator.last), "candidate-credential-secret") {
		t.Fatalf("complete candidate was not hydrated: calls=%d candidate=%s", fixture.hydrator.calls, fixture.validator.last)
	}
	var unchangedTLS model.Tls
	_ = fixture.db.First(&unchangedTLS, 11).Error
	if string(unchangedTLS.Server) != string(beforeTLS.Server) {
		t.Fatal("preview wrote TLS state")
	}
	checkpoint, err := prepareCheckpoint(t, fixture, preview, tlsEndpoint(), false)
	if err != nil {
		t.Fatal(err)
	}
	var row model.InboundFallbackCheckpoint
	if err = fixture.db.First(&row, "id = ?", checkpoint.CheckpointID).Error; err != nil {
		t.Fatal(err)
	}
	serialized := string(row.Payload)
	for _, forbidden := range []string{"private-secret", "short-secret", "certificate-secret", "key-secret", "candidate-user-secret", "candidate-credential-secret", `C:\\`, "/tmp/"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("checkpoint leaked protected content: %s", forbidden)
		}
	}
	apply, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: tlsEndpoint(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.coordinator.calls != 1 || fixture.runtime.applyCalls != 1 || fixture.hooks.beforeCalls != 1 || fixture.hooks.afterCalls != 1 {
		t.Fatalf("mutation path calls: coordinator=%d runtime=%d hooks=%d/%d", fixture.coordinator.calls, fixture.runtime.applyCalls, fixture.hooks.beforeCalls, fixture.hooks.afterCalls)
	}
	if _, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: strings.Repeat("f", 64),
		ApprovedEndpoint: tlsEndpoint(),
	}); !IsAdapterError(err, ErrorStaleBeforeRevision) {
		t.Fatalf("duplicate apply accepted wrong before revision: %v", err)
	}
	wrongEndpoint := tlsEndpoint()
	wrongEndpoint.Port++
	if _, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: wrongEndpoint,
	}); !IsAdapterError(err, ErrorInvalidEndpoint) {
		t.Fatalf("duplicate apply accepted wrong endpoint binding: %v", err)
	}
	duplicate, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: tlsEndpoint(),
	})
	if err != nil || !duplicate.AlreadyCommitted || fixture.runtime.applyCalls != 1 {
		t.Fatalf("duplicate apply = %#v, err=%v, runtime calls=%d", duplicate, err, fixture.runtime.applyCalls)
	}
	var appliedTLS model.Tls
	_ = fixture.db.First(&appliedTLS, 11).Error
	var applied map[string]any
	_ = json.Unmarshal(appliedTLS.Server, &applied)
	reality := applied["reality"].(map[string]any)
	handshake := reality["handshake"].(map[string]any)
	if handshake["server"] != "127.0.0.1" || handshake["server_port"] != float64(19443) || reality["private_key"] != "private-secret" {
		t.Fatalf("applied TLS = %#v", applied)
	}
	verified, err := fixture.service.VerifyEffective(t.Context(), VerifyEffectiveRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedAfterRevision: apply.AfterConfigurationRevision,
		ExpectedEffectiveRevision: apply.ExpectedEffectiveRevision,
	})
	if err != nil || !verified.Verified {
		t.Fatalf("verification = %#v, err=%v", verified, err)
	}
	if _, err = fixture.service.ReleaseCheckpoint(t.Context(), ReleaseCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, Kind: CheckpointProofDurablyAdopted, ProofDigest: strings.Repeat("c", 64),
	}); !IsAdapterError(err, ErrorCheckpointRelease) {
		t.Fatalf("unverified adoption released checkpoint: %v", err)
	}
	restored, err := fixture.service.RestoreCheckpoint(t.Context(), RestoreCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedCurrentRevision: apply.AfterConfigurationRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.RestoredConfigurationRevision != preview.BeforeConfigurationRevision {
		t.Fatalf("restore revision = %s", restored.RestoredConfigurationRevision)
	}
	var restoredTLS model.Tls
	_ = fixture.db.First(&restoredTLS, 11).Error
	var restoredObject map[string]any
	_ = json.Unmarshal(restoredTLS.Server, &restoredObject)
	restoredHandshake := restoredObject["reality"].(map[string]any)["handshake"].(map[string]any)
	if restoredHandshake["server_port"] != float64(18443) {
		t.Fatalf("restored TLS = %#v", restoredObject)
	}
	release, err := fixture.service.ReleaseCheckpoint(t.Context(), ReleaseCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, Kind: CheckpointProofRestoreVerified, ProofDigest: restored.ProofDigest,
	})
	if err != nil || release.CheckpointID == "" {
		t.Fatalf("release = %#v, err=%v", release, err)
	}
	if _, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: tlsEndpoint(),
	}); !IsAdapterError(err, ErrorMutationConflict) {
		t.Fatalf("released checkpoint apply error = %v", err)
	}
}

func TestTrojanTypedVariantsChangeOnlyAllowedSubtrees(t *testing.T) {
	tests := []struct {
		name           string
		variant        FallbackPatchVariantV1
		replaceDefault bool
	}{
		{name: "default", variant: FallbackPatchTrojanDefaultTCP},
		{name: "alpn", variant: FallbackPatchTrojanALPNTCP, replaceDefault: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPatchFixture(t, trojanRow())
			var before model.Inbound
			_ = fixture.db.First(&before, 32).Error
			request := previewRequest(t, fixture, test.variant, plaintextEndpoint(), test.replaceDefault)
			preview, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), test.replaceDefault)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
				CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
				ApprovedEndpoint: plaintextEndpoint(),
			}); err != nil {
				t.Fatal(err)
			}
			var after model.Inbound
			_ = fixture.db.First(&after, 32).Error
			var beforeObject, afterObject map[string]json.RawMessage
			_ = json.Unmarshal(before.Options, &beforeObject)
			_ = json.Unmarshal(after.Options, &afterObject)
			for _, field := range []string{"listen", "listen_port", "tcp_fast_open"} {
				if string(beforeObject[field]) != string(afterObject[field]) {
					t.Fatalf("unrelated field %s changed", field)
				}
			}
			if test.variant == FallbackPatchTrojanDefaultTCP && string(beforeObject["fallback_for_alpn"]) != string(afterObject["fallback_for_alpn"]) {
				t.Fatal("default patch changed ALPN map")
			}
			if test.variant == FallbackPatchTrojanALPNTCP {
				var fallbackMap map[string]checkpointTargetV1
				_ = json.Unmarshal(afterObject["fallback_for_alpn"], &fallbackMap)
				if len(fallbackMap) != 2 || fallbackMap["h2"].Port != 19080 || fallbackMap["http/1.1"].Port != 19080 {
					t.Fatalf("ALPN map = %#v", fallbackMap)
				}
			}
		})
	}
}

func TestPreviewAndCheckpointFailClosed(t *testing.T) {
	t.Run("shared TLS", func(t *testing.T) {
		fixture := newPatchFixture(t, realityRow())
		other := model.Inbound{Id: 99, Type: "http", Tag: "other-inbound", TlsId: 11, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":28080}`)}
		if err := fixture.db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		request := previewRequest(t, fixture, FallbackPatchVLESSRealityHandshakeTCP, tlsEndpoint(), false)
		if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorSharedTLS) {
			t.Fatalf("shared TLS error = %v", err)
		}
	})

	t.Run("reference added after preview", func(t *testing.T) {
		fixture := newPatchFixture(t, realityRow())
		request := previewRequest(t, fixture, FallbackPatchVLESSRealityHandshakeTCP, tlsEndpoint(), false)
		preview, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, err := prepareCheckpoint(t, fixture, preview, tlsEndpoint(), false)
		if err != nil {
			t.Fatal(err)
		}
		other := model.Inbound{Id: 100, Type: "http", Tag: "late-reference", TlsId: 11, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":28081}`)}
		if err = fixture.db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		_, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: tlsEndpoint(),
		})
		if !IsAdapterError(err, ErrorStaleBeforeRevision) && !IsAdapterError(err, ErrorSharedTLS) {
			t.Fatalf("late reference error = %v", err)
		}
	})

	t.Run("stale and invalid endpoint", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		request.Expected.ConfigurationRevision = strings.Repeat("0", 64)
		if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorStalePreview) {
			t.Fatalf("stale error = %v", err)
		}
		request = previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		request.ApprovedEndpoint.Bind = "/run/target.sock"
		if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorInvalidEndpoint) {
			t.Fatalf("endpoint error = %v", err)
		}

		request = previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
		checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
		changedEndpoint := plaintextEndpoint()
		changedEndpoint.Port++
		if _, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: changedEndpoint,
		}); !IsAdapterError(err, ErrorInvalidEndpoint) {
			t.Fatalf("endpoint binding error = %v", err)
		}
	})

	t.Run("partial ALPN and transport", func(t *testing.T) {
		inbound := trojanRow()
		inbound.Options = json.RawMessage(`{"listen":"127.0.0.1","listen_port":25443,"fallback":{"server":"127.0.0.1","server_port":18080},"fallback_for_alpn":{"h2":{"server":"127.0.0.1","server_port":18081}}}`)
		fixture := newPatchFixture(t, inbound)
		request := previewRequest(t, fixture, FallbackPatchTrojanALPNTCP, plaintextEndpoint(), false)
		if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorUnsupportedConfig) {
			t.Fatalf("partial ALPN error = %v", err)
		}
	})

	t.Run("tamper and expiry", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
		checkpoint, err := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
		if err != nil {
			t.Fatal(err)
		}
		if err = fixture.db.Model(&model.InboundFallbackCheckpoint{}).Where("id = ?", checkpoint.CheckpointID).
			Update("payload", []byte(`{"tampered":true}`)).Error; err != nil {
			t.Fatal(err)
		}
		_, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorCheckpointTampered) {
			t.Fatalf("tamper error = %v", err)
		}

		fresh := newPatchFixture(t, trojanRow())
		freshRequest := previewRequest(t, fresh, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		freshPreview, _ := fresh.service.PreviewFallbackPatch(t.Context(), freshRequest)
		freshCheckpoint, _ := prepareCheckpoint(t, fresh, freshPreview, plaintextEndpoint(), false)
		fresh.now = fresh.now.Add(previewLifetime + time.Second)
		_, err = fresh.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: freshCheckpoint.CheckpointID, ExpectedBeforeRevision: freshPreview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorCheckpointStale) {
			t.Fatalf("expiry error = %v", err)
		}
	})
}

func TestMutationFailuresRollbackOrRetainRecovery(t *testing.T) {
	t.Run("candidate validation", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		fixture.validator.fail = true
		if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorInvalidCandidate) {
			t.Fatalf("validation error = %v", err)
		}
		var count int64
		_ = fixture.db.Model(&model.InboundFallbackCheckpoint{}).Count(&count).Error
		if count != 0 || fixture.runtime.applyCalls != 0 {
			t.Fatal("validation failure mutated state")
		}
	})

	t.Run("validation and transaction", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
		checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
		var before model.Inbound
		_ = fixture.db.First(&before, 32).Error
		fixture.hooks.failBefore = true
		_, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorDatabase) {
			t.Fatalf("transaction error = %v", err)
		}
		var after model.Inbound
		_ = fixture.db.First(&after, 32).Error
		if string(before.Options) != string(after.Options) || fixture.runtime.applyCalls != 0 {
			t.Fatal("failed transaction changed database or runtime")
		}
	})

	t.Run("runtime failure keeps checkpoint", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
		checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
		fixture.runtime.applyErr = errors.New("injected runtime failure")
		_, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorRuntimeApply) {
			t.Fatalf("runtime error = %v", err)
		}
		var row model.InboundFallbackCheckpoint
		_ = fixture.db.First(&row, "id = ?", checkpoint.CheckpointID).Error
		if row.State != checkpointStateCommitted {
			t.Fatalf("checkpoint state = %s", row.State)
		}
		if _, err = fixture.service.ReleaseCheckpoint(t.Context(), ReleaseCheckpointRequestV1{
			CheckpointID: checkpoint.CheckpointID, Kind: CheckpointProofDurablyAdopted, ProofDigest: strings.Repeat("c", 64),
		}); !IsAdapterError(err, ErrorCheckpointRelease) {
			t.Fatalf("unknown runtime release error = %v", err)
		}
	})

	t.Run("cancellation boundaries", func(t *testing.T) {
		fixture := newPatchFixture(t, trojanRow())
		request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
		checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := fixture.service.ApplyFallbackPatch(cancelled, ApplyFallbackPatchRequestV1{
			CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorCancelled) || fixture.coordinator.calls != 0 {
			t.Fatalf("pre-commit cancellation = %v, coordinator=%d", err, fixture.coordinator.calls)
		}

		ambiguous := newPatchFixture(t, trojanRow())
		ambiguousRequest := previewRequest(t, ambiguous, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
		ambiguousPreview, _ := ambiguous.service.PreviewFallbackPatch(t.Context(), ambiguousRequest)
		ambiguousCheckpoint, _ := prepareCheckpoint(t, ambiguous, ambiguousPreview, plaintextEndpoint(), false)
		ctx, cancelAfterCommit := context.WithCancel(t.Context())
		ambiguous.runtime.cancel = cancelAfterCommit
		_, err = ambiguous.service.ApplyFallbackPatch(ctx, ApplyFallbackPatchRequestV1{
			CheckpointID: ambiguousCheckpoint.CheckpointID, ExpectedBeforeRevision: ambiguousPreview.BeforeConfigurationRevision,
			ApprovedEndpoint: plaintextEndpoint(),
		})
		if !IsAdapterError(err, ErrorAmbiguousResult) {
			t.Fatalf("post-commit cancellation = %v", err)
		}
	})
}

func TestRestoreRefusesConcurrentDrift(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
	checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
	apply, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: plaintextEndpoint(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var inbound model.Inbound
	_ = fixture.db.First(&inbound, 32).Error
	object, _ := decodeObject(inbound.Options)
	object["tcp_fast_open"] = json.RawMessage("false")
	drifted, _ := json.Marshal(object)
	if err = fixture.db.Model(&model.Inbound{}).Where("id = ?", 32).Update("options", drifted).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.service.RestoreCheckpoint(t.Context(), RestoreCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedCurrentRevision: apply.AfterConfigurationRevision,
	}); !IsAdapterError(err, ErrorReconcileRequired) {
		t.Fatalf("restore drift error = %v", err)
	}
	var current model.Inbound
	_ = fixture.db.First(&current, 32).Error
	if !strings.Contains(string(current.Options), `"tcp_fast_open":false`) {
		t.Fatal("restore overwrote concurrent edit")
	}
}

func TestEffectiveVerificationRejectsPresenceOnlyAndDrift(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
	checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
	apply, err := fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: plaintextEndpoint(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.observation.OptionsDigest = ""
	if _, err = fixture.service.VerifyEffective(t.Context(), VerifyEffectiveRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedAfterRevision: apply.AfterConfigurationRevision,
		ExpectedEffectiveRevision: apply.ExpectedEffectiveRevision,
	}); !IsAdapterError(err, ErrorEffectiveVerify) {
		t.Fatalf("presence-only error = %v", err)
	}
	fixture.runtime.observation.OptionsDigest = strings.Repeat("d", 64)
	fixture.runtime.observation.ManagerGeneration++
	if _, err = fixture.service.VerifyEffective(t.Context(), VerifyEffectiveRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedAfterRevision: apply.AfterConfigurationRevision,
		ExpectedEffectiveRevision: apply.ExpectedEffectiveRevision,
	}); !IsAdapterError(err, ErrorEffectiveVerify) {
		t.Fatalf("generation drift error = %v", err)
	}
}

func TestApplyRejectsRuntimeDigestThatDoesNotMatchCandidate(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	preview, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.preserveOptionsDigest = true
	fixture.runtime.observation.OptionsDigest = strings.Repeat("e", 64)
	if _, err = fixture.service.ApplyFallbackPatch(t.Context(), ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: preview.BeforeConfigurationRevision,
		ApprovedEndpoint: plaintextEndpoint(),
	}); !IsAdapterError(err, ErrorEffectiveVerify) {
		t.Fatalf("mismatched runtime digest error = %v", err)
	}
	var row model.InboundFallbackCheckpoint
	if err = fixture.db.First(&row, "id = ?", checkpoint.CheckpointID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != checkpointStateCommitted {
		t.Fatalf("checkpoint state after runtime mismatch = %s", row.State)
	}
}

func TestUncommittedCheckpointReleaseIsProofBound(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	preview, _ := fixture.service.PreviewFallbackPatch(t.Context(), request)
	checkpoint, _ := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
	if _, err := fixture.service.ReleaseCheckpoint(t.Context(), ReleaseCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, Kind: CheckpointProofDurablyAdopted,
		ProofDigest: strings.Repeat("c", 64),
	}); !IsAdapterError(err, ErrorCheckpointRelease) {
		t.Fatalf("unverified prepared adoption release error = %v", err)
	}
	if _, err := fixture.service.ReleaseCheckpoint(t.Context(), ReleaseCheckpointRequestV1{
		CheckpointID: checkpoint.CheckpointID, Kind: CheckpointProofApplyNeverCommitted,
		ProofDigest: checkpoint.UncommittedReleaseProof,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFindCheckpointByPreviewDigestIsBoundedExactAndAmbiguitySafe(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	preview, err := fixture.service.PreviewFallbackPatch(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false)
	if err != nil {
		t.Fatal(err)
	}
	found, err := fixture.service.FindCheckpoint(t.Context(), FindCheckpointRequestV1{PreviewDigest: preview.Digest})
	if err != nil || found.CheckpointID != checkpoint.CheckpointID || found.State != CheckpointStatePrepared ||
		found.IntegrityDigest != checkpoint.IntegrityDigest || found.UncommittedReleaseProof != checkpoint.UncommittedReleaseProof {
		t.Fatalf("found=%#v err=%v", found, err)
	}
	if _, err := fixture.service.FindCheckpoint(t.Context(), FindCheckpointRequestV1{PreviewDigest: strings.Repeat("0", 64)}); !IsAdapterError(err, ErrorCheckpointMissing) {
		t.Fatalf("missing digest error=%v", err)
	}
	if _, err := prepareCheckpoint(t, fixture, preview, plaintextEndpoint(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.FindCheckpoint(t.Context(), FindCheckpointRequestV1{PreviewDigest: preview.Digest}); !IsAdapterError(err, ErrorMutationConflict) {
		t.Fatalf("ambiguous digest error=%v", err)
	}
}

func TestDefaultCandidateValidatorUsesPinnedUnmarshallerAndDryCheck(t *testing.T) {
	validator := defaultCandidateValidator{}
	valid := []byte(`{"type":"http","tag":"candidate-inbound","listen":"127.0.0.1","listen_port":18080}`)
	if err := validator.ValidateInbound(t.Context(), valid); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	malformed := []byte(`{"type":"http","tag":"candidate-inbound","listen_port":18080,"unknown_candidate_field":true}`)
	if err := validator.ValidateInbound(t.Context(), malformed); err == nil {
		t.Fatal("unknown candidate field was accepted")
	}
}
