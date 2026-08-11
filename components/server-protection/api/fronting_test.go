//go:build !minimal

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
)

type frontingSemanticRecorder struct {
	preview  protectionfronting.FrontingPreviewRequestV2
	prepare  protectionfronting.FrontingPrepareRequestV2
	apply    protectionfronting.FrontingApplyRequestV2
	rollback protectionfronting.FrontingRollbackRequestV2
	actor    string
	calls    []string
}

func (r *frontingSemanticRecorder) Status(context.Context) (protectionfronting.FrontingStatusPageV2, error) {
	r.calls = append(r.calls, "status")
	return protectionfronting.FrontingStatusPageV2{Items: []protectionfronting.FrontingStatusV2{{
		Schema: protectionfronting.FrontingSemanticStatusSchemaV2, ResourceID: "fixture:public", DisplayIdentity: "Fixture public",
		ActualState: "NOT_APPLIED", ApplyGate: "EXPERIMENTAL_DISABLED_BY_DEFAULT", Blocks: []string{}, Warnings: []string{}, ReasonCodes: []string{},
	}}, GeneratedAt: 1}, nil
}
func (r *frontingSemanticRecorder) Preview(_ context.Context, value protectionfronting.FrontingPreviewRequestV2) (protectionfronting.FrontingStrategyPlanV2, error) {
	r.calls, r.preview = append(r.calls, "preview"), value
	return protectionfronting.FrontingStrategyPlanV2{PlanID: strings.Repeat("a", 64), CanonicalPlanDigest: strings.Repeat("a", 64), Safety: protectionfronting.FrontingSafetyPlanFactsV2{Blocks: []string{}, Warnings: []string{}, ReasonCodes: []string{}}, Strategy: protectionfronting.StrategyProjectionV2{Actual: protectionfronting.FrontingActualNotAppliedV2}}, nil
}
func (r *frontingSemanticRecorder) Prepare(_ context.Context, value protectionfronting.FrontingPrepareRequestV2, actor string) (protectionfronting.FrontingOperationViewV2, error) {
	r.calls, r.prepare, r.actor = append(r.calls, "prepare"), value, actor
	return semanticOperation("PREPARED"), nil
}
func (r *frontingSemanticRecorder) Apply(_ context.Context, value protectionfronting.FrontingApplyRequestV2) (protectionfronting.FrontingOperationViewV2, error) {
	r.calls, r.apply = append(r.calls, "apply"), value
	return semanticOperation("APPLIED"), nil
}
func (r *frontingSemanticRecorder) Rollback(_ context.Context, value protectionfronting.FrontingRollbackRequestV2) (protectionfronting.FrontingOperationViewV2, error) {
	r.calls, r.rollback = append(r.calls, "rollback"), value
	return semanticOperation("ROLLED_BACK"), nil
}
func (r *frontingSemanticRecorder) Operation(_ context.Context, operationID string) (protectionfronting.FrontingOperationViewV2, error) {
	r.calls = append(r.calls, "operation")
	value := semanticOperation("PREPARED")
	value.OperationID = operationID
	return value, nil
}
func (r *frontingSemanticRecorder) Recovery(_ context.Context, operationID string) (protectionfronting.FrontingRecoveryStatusV2, error) {
	r.calls = append(r.calls, "recovery")
	return protectionfronting.FrontingRecoveryStatusV2{OperationID: operationID, OperationRevision: 2, PermittedNextAction: "APPLY_OR_ROLLBACK", ReasonCodes: []string{}}, nil
}

func semanticOperation(actual string) protectionfronting.FrontingOperationViewV2 {
	return protectionfronting.FrontingOperationViewV2{OperationID: "fronting-operation", OperationRevision: 2, ResourceID: "fixture:public", WorkflowState: strings.ToLower(actual), ActualState: actual, PlanDigest: strings.Repeat("a", 64), Leases: []protectionfronting.FrontingLeaseSummaryV2{}, ReasonCodes: []string{}, SafeNextAction: "REFRESH"}
}

func newSemanticRouter(t *testing.T, scope string, recorder frontingSemanticService) (*ginRouterFacade, *[]auditEvent, *protectionrepositoryFacade) {
	router, audits, _, repository, _ := newProtectionAPIRouterWithDBAndSemantic(t, scope, nil, recorder)
	return &ginRouterFacade{router}, audits, &protectionrepositoryFacade{repository}
}

// Small facades keep the tests focused on the HTTP contract without exporting
// test-only constructors from production packages.
type ginRouterFacade struct{ http.Handler }
type protectionrepositoryFacade struct {
	repository interface {
		LoadSettingsRevision(context.Context) (domain.Settings, int, bool, error)
		SaveSettingsRevision(context.Context, domain.Settings, int) (int, error)
	}
}

func TestFrontingSemanticScopesAndStrictPreview(t *testing.T) {
	readRecorder := &frontingSemanticRecorder{}
	readRouter, _, _ := newSemanticRouter(t, readScope, readRecorder)
	if response := requestProtectionAPI(readRouter, http.MethodGet, "/api/components/server-protection/fronting/status", ""); response.Code != http.StatusOK {
		t.Fatalf("status=%d %s", response.Code, response.Body.String())
	}
	if response := requestProtectionAPI(readRouter, http.MethodPost, "/api/components/server-protection/fronting/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read preview=%d", response.Code)
	}

	writeRecorder := &frontingSemanticRecorder{}
	writeRouter, audits, _ := newSemanticRouter(t, writeScope, writeRecorder)
	body := `{"resourceId":"fixture:public","expectedCurrentConfigurationRevision":"` + strings.Repeat("a", 64) + `","requestedStrategy":"L4_ONE_TO_ONE_FRONTING","socketClaim":{"resourceId":"fixture:public","endpointId":"public-tcp","claimRevision":"` + strings.Repeat("b", 64) + `"},"backendReferences":[],"fallbackReferences":[],"selectedProxyMode":"OFF","selectors":[],"default":{"policy":"REJECT"}}`
	if response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/fronting/preview", body); response.Code != http.StatusOK {
		t.Fatalf("preview=%d %s", response.Code, response.Body.String())
	}
	if writeRecorder.preview.ResourceID != "fixture:public" || len(writeRecorder.calls) != 1 || writeRecorder.calls[0] != "preview" {
		t.Fatalf("preview calls=%#v request=%#v", writeRecorder.calls, writeRecorder.preview)
	}
	if len(*audits) != 2 || (*audits)[0].Name != "server_protection_fronting_preview_requested" || (*audits)[1].Name != "server_protection_fronting_preview_completed" {
		t.Fatalf("audits=%#v", *audits)
	}
	for _, invalid := range []string{
		strings.TrimSuffix(body, "}") + `,"unknown":"field"}`,
		`{"routes":[{"listen":{"address":"0.0.0.0","port":443},"sni":["panel.example"],"alpn":["h2"]}]}`,
	} {
		response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/fronting/preview", invalid)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
			t.Fatalf("invalid preview=%d %s", response.Code, response.Body.String())
		}
	}
}

func TestFrontingMutationsRequireExactApplyScopeAndLegacySyncIsRetired(t *testing.T) {
	for _, scope := range []string{readScope, writeScope, "admin"} {
		router, _, _ := newSemanticRouter(t, scope, &frontingSemanticRecorder{})
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/fronting/prepare", `{}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("scope %s prepare=%d", scope, response.Code)
		}
	}
	recorder := &frontingSemanticRecorder{}
	router, audits, repository := newSemanticRouter(t, applyScope, recorder)
	settings, revision, _, err := repository.repository.LoadSettingsRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled, settings.FeatureFlags["enable_fronting_beta"], settings.AdvancedAcknowledgedAt = true, true, 1
	if _, err := repository.repository.SaveSettingsRevision(context.Background(), settings, revision); err != nil {
		t.Fatal(err)
	}

	digest := strings.Repeat("a", 64)
	prepare := `{"planId":"` + digest + `","planDigest":"` + digest + `","resourceId":"fixture:public","runtimeIdentityRevision":"` + digest + `","strategyCapabilityRevision":"` + digest + `","socketClaimRevision":"` + digest + `","selectorSetRevision":"","targetReferenceRevisions":["` + digest + `"],"idempotencyKey":"prepare-key","experimentalRiskAcknowledged":true,"acknowledgement":"PREPARE FRONTING ` + digest + `"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/fronting/prepare", prepare); response.Code != http.StatusOK {
		t.Fatalf("prepare=%d %s", response.Code, response.Body.String())
	}
	apply := `{"operationId":"fronting-operation","operationRevision":2,"planDigest":"` + digest + `","targetAuthorityRevisions":["lease-r2"],"idempotencyKey":"apply-key","confirmation":"APPLY FRONTING fronting-operation"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/fronting/apply", apply); response.Code != http.StatusOK {
		t.Fatalf("apply=%d %s", response.Code, response.Body.String())
	}
	rollback := `{"operationId":"fronting-operation","operationRevision":3,"idempotencyKey":"rollback-key","confirmation":"ROLLBACK FRONTING fronting-operation"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/fronting/rollback", rollback); response.Code != http.StatusOK {
		t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
	}
	if recorder.actor != "tester" || recorder.apply.OperationRevision != 2 || recorder.rollback.OperationRevision != 3 {
		t.Fatalf("recorder=%#v", recorder)
	}
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/fronting/sync", `{"listen":"0.0.0.0:443"}`); response.Code != http.StatusGone || !strings.Contains(response.Body.String(), "legacy_fronting_write_retired") {
		t.Fatalf("sync=%d %s", response.Code, response.Body.String())
	}
	if len(recorder.calls) != 3 {
		t.Fatalf("retired route invoked semantic service: %#v", recorder.calls)
	}
	for _, audit := range *audits {
		encoded := strings.ToLower(audit.Name)
		for key := range audit.Details {
			encoded += " " + strings.ToLower(key)
		}
		for _, forbidden := range []string{"config", "selector", "target", "lease", "path", "secret"} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("unsafe audit=%#v", audit)
			}
		}
	}
}

func TestFrontingOperationAndRecoveryAreReadOnly(t *testing.T) {
	recorder := &frontingSemanticRecorder{}
	router, _, _ := newSemanticRouter(t, readScope, recorder)
	for _, suffix := range []string{"", "/recovery"} {
		response := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/fronting/operations/fronting-operation"+suffix, "")
		if response.Code != http.StatusOK || strings.Contains(strings.ToLower(response.Body.String()), "config") || strings.Contains(strings.ToLower(response.Body.String()), "path") {
			t.Fatalf("inspection %s=%d %s", suffix, response.Code, response.Body.String())
		}
	}
	if strings.Join(recorder.calls, ",") != "operation,recovery" {
		t.Fatalf("calls=%#v", recorder.calls)
	}
}
