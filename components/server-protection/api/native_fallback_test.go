//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionnativefallback "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/nativefallback"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"github.com/gin-gonic/gin"
)

type nativeFallbackRecorder struct {
	previewCalls  int
	prepareCalls  int
	applyCalls    int
	rollbackCalls int
}

func (*nativeFallbackRecorder) Inspect(_ context.Context, inboundID uint) (coreinboundcontrol.CoreRuntimeIdentityV1, coreinboundcontrol.InboundFallbackSnapshotV1, error) {
	revision := strings.Repeat("a", 64)
	return coreinboundcontrol.CoreRuntimeIdentityV1{State: coreinboundcontrol.RuntimeIdentityVerified, IdentityRevision: revision},
		coreinboundcontrol.InboundFallbackSnapshotV1{
			InboundDatabaseID: inboundID, ResourceID: "core:inbound:1", Tag: "fixture-native", Type: "trojan",
			ConfigurationRevision: strings.Repeat("c", 64), CapabilityResolverRevision: strings.Repeat("b", 64),
			Effective: coreinboundcontrol.EffectiveInboundV1{Revision: strings.Repeat("d", 64)},
			Capability: coreinboundcontrol.NativeFallbackCapabilityV1{
				Disposition: coreinboundcontrol.CapabilitySupported, Variant: coreinboundcontrol.NativeFallbackTrojanDefaultTCP,
				NaturalInvalidTrafficFallback: true,
			},
		}, nil
}

func (recorder *nativeFallbackRecorder) Preview(_ context.Context, request protectionnativefallback.PlanRequestV1) (domain.NativeFallbackPlanV1, error) {
	recorder.previewCalls++
	return domain.NativeFallbackPlanV1{
		ActualState: domain.NativeActualNotApplied, DesiredState: domain.NativeFallbackDesired,
		Resource: domain.NativeFallbackResourceBindingV1{ResourceID: request.ExpectedResourceID},
	}, nil
}

func (recorder *nativeFallbackRecorder) Prepare(context.Context, protectionnativefallback.PrepareWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error) {
	recorder.prepareCalls++
	return protectionnativefallback.WorkflowResultV1{}, nil
}

func (recorder *nativeFallbackRecorder) Apply(context.Context, protectionnativefallback.ApplyWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error) {
	recorder.applyCalls++
	return protectionnativefallback.WorkflowResultV1{}, nil
}

func (recorder *nativeFallbackRecorder) Rollback(context.Context, protectionnativefallback.RollbackWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error) {
	recorder.rollbackCalls++
	return protectionnativefallback.WorkflowResultV1{}, nil
}

type nativeResourceContributor struct{ owner string }

func (c nativeResourceContributor) Owner() string { return c.owner }
func (c nativeResourceContributor) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	return []hostresources.ProtectableResource{{
		ID: "core:inbound:1", Kind: "inbound", Owner: c.owner, Name: "fixture-native", InboundTag: "fixture-native",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: strings.Repeat("c", 64)},
	}}, nil
}

func newNativeFallbackRouter(t *testing.T, scope string, native nativeFallbackService) (*gin.Engine, *[]auditEvent, *protectionrepository.Repository) {
	t.Helper()
	_, audits, manager, repository, _ := newProtectionAPIRouterWithDB(t, scope, protectionfronting.NewNginxAdapter())
	owner := "native-api-" + strconv.FormatUint(fixtureContributorSequence.Add(1), 10)
	unregister, err := hostresources.Register(nativeResourceContributor{owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Repository: repository, Operations: manager, Fronting: protectionfronting.NewNginxAdapter(), NativeFallback: native,
		RequireScope: func(c *gin.Context, _ string, allowed ...string) bool {
			for _, candidate := range allowed {
				if candidate == scope {
					return true
				}
			}
			c.AbortWithStatus(http.StatusForbidden)
			return false
		},
		Actor: func(*gin.Context) string { return "native-tester" },
		Audit: func(_ *gin.Context, _ string, event string, _ string, _ string, details map[string]any) {
			*audits = append(*audits, auditEvent{Name: event, Details: details})
		},
		JSONObj: func(c *gin.Context, value interface{}, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": errorString(err), "obj": value})
		},
		JSONMsg: func(c *gin.Context, msg string, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": msg + errorString(err), "obj": nil})
		},
	})
	return router, audits, repository
}

func TestNativeFallbackRouteScopeAndMethodMatrix(t *testing.T) {
	recorder := &nativeFallbackRecorder{}
	readRouter, _, _ := newNativeFallbackRouter(t, readScope, recorder)
	if response := requestProtectionAPI(readRouter, http.MethodGet, "/api/components/server-protection/native-fallback/status", ""); response.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestProtectionAPI(readRouter, http.MethodPost, "/api/components/server-protection/native-fallback/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read preview=%d", response.Code)
	}
	if response := requestProtectionAPI(readRouter, http.MethodGet, "/api/components/server-protection/native-fallback/preview", ""); response.Code == http.StatusOK {
		t.Fatal("unsupported preview method was accepted")
	}

	writeRouter, audits, _ := newNativeFallbackRouter(t, writeScope, recorder)
	body, _ := json.Marshal(nativeFallbackPreviewRequest{
		ResourceID: "core:inbound:1", ExpectedConfigRevision: strings.Repeat("c", 64), TargetReference: nativeTestReference(),
	})
	if response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/native-fallback/preview", string(body)); response.Code != http.StatusOK {
		t.Fatalf("write preview=%d body=%s", response.Code, response.Body.String())
	}
	if recorder.previewCalls != 1 {
		t.Fatalf("preview calls=%d", recorder.previewCalls)
	}
	if len(*audits) != 2 || (*audits)[0].Name != "server_protection_native_fallback_preview_requested" || (*audits)[1].Name != "server_protection_native_fallback_preview_completed" {
		t.Fatalf("preview audit events=%#v", *audits)
	}
	auditJSON, _ := json.Marshal(audits)
	for _, forbidden := range []string{"provider-revision", strings.Repeat("1", 64), "targetReference"} {
		if strings.Contains(string(auditJSON), forbidden) {
			t.Fatalf("preview audit leaked request detail %q: %s", forbidden, auditJSON)
		}
	}
	if response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/native-fallback/prepare", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("write prepare=%d", response.Code)
	}

	applyRouter, _, _ := newNativeFallbackRouter(t, applyScope, recorder)
	if response := requestProtectionAPI(applyRouter, http.MethodPost, "/api/components/server-protection/native-fallback/prepare", `{}`); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "experimental_ack_required") {
		t.Fatalf("apply prepare acknowledgement=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNativeFallbackStrictBodiesExactConfirmationAndRedaction(t *testing.T) {
	recorder := &nativeFallbackRecorder{}
	writeRouter, _, _ := newNativeFallbackRouter(t, writeScope, recorder)
	referenceJSON, _ := json.Marshal(nativeTestReference())
	body := `{"resourceId":"core:inbound:1","expectedConfigRevision":"` + strings.Repeat("c", 64) + `","targetReference":` + string(referenceJSON) + `,"host":"127.0.0.1"}`
	if response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/native-fallback/preview", body); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
		t.Fatalf("unknown host field=%d body=%s", response.Code, response.Body.String())
	}
	applyRouter, audits, _ := newNativeFallbackRouter(t, applyScope, recorder)
	mutation := `{"operationId":"operation-one","operationRevision":1,"planDigest":"` + strings.Repeat("a", 64) + `","providerReservationRevision":"reservation-revision","idempotencyKey":"idempotency-key-one","confirmation":" apply native fallback operation-one "}`
	response := requestProtectionAPI(applyRouter, http.MethodPost, "/api/components/server-protection/native-fallback/apply", mutation)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "confirmation_mismatch") || recorder.applyCalls != 0 {
		t.Fatalf("weakened confirmation=%d calls=%d body=%s", response.Code, recorder.applyCalls, response.Body.String())
	}
	exactMutation := strings.Replace(mutation, `"confirmation":" apply native fallback operation-one "`, `"confirmation":"APPLY NATIVE FALLBACK operation-one"`, 1)
	response = requestProtectionAPI(applyRouter, http.MethodPost, "/api/components/server-protection/native-fallback/apply", exactMutation)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "apply_gate_disabled") || len(*audits) != 2 {
		t.Fatalf("apply gate audit=%d events=%#v body=%s", response.Code, *audits, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "sqlite") || strings.Contains(response.Body.String(), "127.0.0.1") {
		t.Fatalf("native error leaked internal data: %s", response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/components/server-protection/native-fallback/preview", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "text/plain")
	contentTypeResponse := httptest.NewRecorder()
	writeRouter.ServeHTTP(contentTypeResponse, request)
	if contentTypeResponse.Code != http.StatusUnsupportedMediaType || !strings.Contains(contentTypeResponse.Body.String(), "malformed_input") {
		t.Fatalf("content type=%d body=%s", contentTypeResponse.Code, contentTypeResponse.Body.String())
	}
	oversized := strings.Repeat("x", nativeFallbackBodyLimit+1)
	request = httptest.NewRequest(http.MethodPost, "/api/components/server-protection/native-fallback/preview", strings.NewReader(oversized))
	request.Header.Set("Content-Type", "application/json")
	sizeResponse := httptest.NewRecorder()
	writeRouter.ServeHTTP(sizeResponse, request)
	if sizeResponse.Code != http.StatusBadRequest || !strings.Contains(sizeResponse.Body.String(), "malformed_input") {
		t.Fatalf("oversized body=%d body=%s", sizeResponse.Code, sizeResponse.Body.String())
	}
}

func TestNativeFallbackPrepareRejectsUnsafeResourceBeforeAudit(t *testing.T) {
	recorder := &nativeFallbackRecorder{}
	router, audits, _ := newNativeFallbackRouter(t, applyScope, recorder)
	digest := strings.Repeat("a", 64)
	body, err := json.Marshal(nativeFallbackPrepareRequest{
		PlanID: digest, PlanDigest: digest, ResourceID: "/private/sensitive-config.key",
		TargetReference: nativeTestReference(), IdempotencyKey: "prepare-secret-path",
		ExperimentalRiskAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/native-fallback/prepare", string(body))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
		t.Fatalf("unsafe resource response=%d body=%s", response.Code, response.Body.String())
	}
	if len(*audits) != 0 || recorder.previewCalls != 0 || recorder.prepareCalls != 0 {
		t.Fatalf("unsafe resource reached audit/workflow: audits=%#v preview=%d prepare=%d", *audits, recorder.previewCalls, recorder.prepareCalls)
	}
}

func TestNativeFallbackMissingStateIsNotAppliedAndAuditIsBounded(t *testing.T) {
	recorder := &nativeFallbackRecorder{}
	router, audits, _ := newNativeFallbackRouter(t, readScope, recorder)
	response := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/native-fallback/status?resource_id=core:inbound:1&limit=1", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"actualState":"NOT_APPLIED"`) || !strings.Contains(response.Body.String(), "state_absent") {
		t.Fatalf("missing state response=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"privateKey", "shortIds", "checkpoint", "artifact", "filesystem"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("status leaked %q: %s", forbidden, response.Body.String())
		}
	}
	if len(*audits) != 0 {
		t.Fatalf("read-only status unexpectedly audited a mutation: %#v", *audits)
	}
}

func TestNativeFallbackErrorCodesRemainSemanticAndBounded(t *testing.T) {
	cases := map[string]string{
		"target_reference_stale":       "target_reference_stale",
		"prepared_plan_expired":        "plan_expired",
		"prepare_idempotency_conflict": "operation_conflict",
		"apply_operation_stale":        "operation_revision_stale",
		"provider_reservation_stale":   "provider_reservation_conflict",
		"health_observation_expired":   "target_health_stale",
		"concurrent_core_drift":        "reconcile_required",
	}
	for workflowCode, expected := range cases {
		t.Run(workflowCode, func(t *testing.T) {
			code, status := nativeErrorCode(&protectionnativefallback.WorkflowError{Code: workflowCode})
			if code != expected || status != http.StatusConflict {
				t.Fatalf("code=%q status=%d, want %q/%d", code, status, expected, http.StatusConflict)
			}
		})
	}
}

func TestNativeFallbackTargetProjectionIsExactAndRedacted(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	target, err := neutralfallback.FinalizeFallbackTargetV2(neutralfallback.FallbackTargetV2{
		Identity: neutralfallback.TargetIdentity{ProviderID: "provider", TargetID: "target"},
		Publish:  neutralfallback.PublishFactsV2{Revision: "publish", ContentDigest: strings.Repeat("1", 64)},
		Endpoint: neutralfallback.EndpointV2{
			EndpointID: "endpoint", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4,
			Address: "127.0.0.1", Port: 8443, Local: true, TransportSecurity: neutralfallback.TransportSecurityTLS,
			ApplicationProtocols: []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP2},
			AcceptedServerNames:  []string{"private.example"}, ProxyProtocol: hostresources.CapabilityNo,
			CanReachManagement: hostresources.CapabilityNo,
		},
		Health:           neutralfallback.HealthV2{Readiness: neutralfallback.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Capacity:         neutralfallback.CapacityV2{State: neutralfallback.CapacityReady, ReservationSlotsTotal: 2, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-revision", Source: "fixture", ConfidenceBP: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := neutralfallback.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exactTarget([]neutralfallback.FallbackTargetV2{target}, reference); !ok {
		t.Fatal("exact target reference was not resolved")
	}
	stale := reference
	stale.ProviderRevision = "provider-revision-stale"
	if _, ok := exactTarget([]neutralfallback.FallbackTargetV2{target}, stale); ok {
		t.Fatal("stale target reference was treated as exact")
	}
	projectedJSON, _ := json.Marshal(projectNativeTarget(target, now))
	for _, forbidden := range []string{"127.0.0.1", "8443", "private.example"} {
		if strings.Contains(string(projectedJSON), forbidden) {
			t.Fatalf("target projection leaked %q: %s", forbidden, projectedJSON)
		}
	}
}

func nativeTestReference() neutralfallback.FallbackTargetReferenceV2 {
	return neutralfallback.FallbackTargetReferenceV2{
		Schema: neutralfallback.TargetReferenceSchemaV2, ProviderID: "provider", TargetID: "target",
		PublishRevision: "publish", ContentDigest: strings.Repeat("1", 64), EndpointID: "endpoint",
		EndpointRevision: strings.Repeat("2", 64), ProviderHealthRevision: strings.Repeat("3", 64),
		CapacityRevision: strings.Repeat("4", 64), ProviderRevision: "provider-revision",
	}
}
