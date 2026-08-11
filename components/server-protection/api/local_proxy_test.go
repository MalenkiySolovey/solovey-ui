//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionlocalproxy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/localproxy"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type localProxyAPIRecorder struct {
	preview protectionlocalproxy.PlanReferenceV1
	prepare protectionlocalproxy.PrepareRequestV1
	apply   protectionlocalproxy.ApplyRequestV1
	disable protectionlocalproxy.DisableRequestV1
}

func (*localProxyAPIRecorder) Status(context.Context, bool) (protectionlocalproxy.StatusV1, error) {
	return protectionlocalproxy.StatusV1{
		Schema: protectionlocalproxy.StatusSchemaV1, GeneratedAt: 1,
		Facts: []hostresources.LocalProxyFactV1{}, Plans: []protectionlocalproxy.PlanV1{},
		States: []protectionlocalproxy.StateViewV1{}, Experimental: true, DefaultApplyEnabled: false,
	}, nil
}
func (r *localProxyAPIRecorder) Preview(_ context.Context, value protectionlocalproxy.PlanReferenceV1) (protectionlocalproxy.PlanV1, error) {
	r.preview = value
	return protectionlocalproxy.PlanV1{
		PlanID: "local-proxy-plan:fixture", PlanDigest: strings.Repeat("a", 64),
		ResourceID: value.ResourceID, EndpointID: value.EndpointID, FactRevision: value.FactRevision,
		ApplyGate: protectionlocalproxy.ApplyGateExperimentalAck, ActualState: protectionlocalproxy.StateNotApplied,
	}, nil
}
func (r *localProxyAPIRecorder) Prepare(_ context.Context, _ string, value protectionlocalproxy.PrepareRequestV1) (protectionlocalproxy.ResultV1, error) {
	r.prepare = value
	return localProxyAPIResult(protectionlocalproxy.StatePrepared), nil
}
func (r *localProxyAPIRecorder) Apply(_ context.Context, value protectionlocalproxy.ApplyRequestV1) (protectionlocalproxy.ResultV1, error) {
	r.apply = value
	return localProxyAPIResult(protectionlocalproxy.StateAppliedExperimental), nil
}
func (r *localProxyAPIRecorder) Disable(_ context.Context, value protectionlocalproxy.DisableRequestV1) (protectionlocalproxy.ResultV1, error) {
	r.disable = value
	return localProxyAPIResult(protectionlocalproxy.StateNotApplied), nil
}
func (*localProxyAPIRecorder) Operation(context.Context, string) (protectionrepository.OperationLockModel, error) {
	return protectionrepository.OperationLockModel{OperationID: "operation-1", Kind: "local_proxy", State: "applied", Revision: 4}, nil
}
func (*localProxyAPIRecorder) Recovery(context.Context, string) (protectionlocalproxy.RecoveryStatusV1, error) {
	return protectionlocalproxy.RecoveryStatusV1{
		OperationID: "operation-1", ActualState: protectionlocalproxy.StateAppliedExperimental,
		ProviderGuarded: true, SafeNextAction: "DISABLE_OR_REFRESH",
	}, nil
}

func localProxyAPIResult(state protectionlocalproxy.ActualState) protectionlocalproxy.ResultV1 {
	return protectionlocalproxy.ResultV1{
		OperationID: "operation-1", OperationRevision: 4, OperationState: strings.ToLower(string(state)),
		PlanID: "local-proxy-plan:fixture", PlanDigest: strings.Repeat("a", 64), ActualState: state,
	}
}

func TestLocalProxyRoutesEnforceScopesAndAcceptOnlySemanticRequests(t *testing.T) {
	digest := strings.Repeat("a", 64)
	readRecorder := &localProxyAPIRecorder{}
	readRouter, _, _, _, _ := newProtectionAPIRouterWithLocalProxy(t, readScope, protectionfronting.NewNginxAdapter(), &frontingSemanticStub{}, readRecorder)
	if response := requestProtectionAPI(readRouter, http.MethodGet, "/api/components/server-protection/local-proxy/status", ""); response.Code != http.StatusOK {
		t.Fatalf("read status=%d %s", response.Code, response.Body.String())
	}
	if response := requestProtectionAPI(readRouter, http.MethodPost, "/api/components/server-protection/local-proxy/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read scope mutated=%d %s", response.Code, response.Body.String())
	}

	writeRecorder := &localProxyAPIRecorder{}
	writeRouter, audits, _, _, _ := newProtectionAPIRouterWithLocalProxy(t, writeScope, protectionfronting.NewNginxAdapter(), &frontingSemanticStub{}, writeRecorder)
	preview := `{"resourceId":"core:inbound:17","endpointId":"tcp:ipv4:1080","factRevision":"` + digest + `"}`
	if response := requestProtectionAPI(writeRouter, http.MethodPost, "/api/components/server-protection/local-proxy/preview", preview); response.Code != http.StatusOK {
		t.Fatalf("preview=%d %s", response.Code, response.Body.String())
	}
	if writeRecorder.preview.ResourceID != "core:inbound:17" || len(*audits) != 1 {
		t.Fatalf("preview=%#v audits=%#v", writeRecorder.preview, *audits)
	}
	auditJSON, _ := json.Marshal(*audits)
	for _, forbidden := range []string{"credential", "password", "authorization", "destination", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(string(auditJSON)), forbidden) {
			t.Fatalf("audit leaked forbidden detail %q: %s", forbidden, auditJSON)
		}
	}
}

func TestLocalProxyAPIRejectsAllDangerousAuthorityInputs(t *testing.T) {
	router, _, _, _, _ := newProtectionAPIRouterWithLocalProxy(t, writeScope, protectionfronting.NewNginxAdapter(), &frontingSemanticStub{}, &localProxyAPIRecorder{})
	digest := strings.Repeat("a", 64)
	base := `{"resourceId":"core:inbound:17","endpointId":"tcp:ipv4:1080","factRevision":"` + digest + `"}`
	for _, field := range []string{"host", "ip", "bind", "port", "url", "domain", "destination", "target", "sink", "username", "password", "credentials", "authorization", "proxyAuthorization", "rawConfig", "listen"} {
		body := strings.TrimSuffix(base, "}") + `,"` + field + `":"forbidden"}`
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/local-proxy/preview", body)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
			t.Fatalf("field %s escaped strict decoder: %d %s", field, response.Code, response.Body.String())
		}
	}
}

func TestLocalProxyMutationRoutesAreExplicitAndRollbackAliasIsNarrow(t *testing.T) {
	recorder := &localProxyAPIRecorder{}
	router, _, _, _, _ := newProtectionAPIRouterWithLocalProxy(t, applyScope, protectionfronting.NewNginxAdapter(), &frontingSemanticStub{}, recorder)
	digest := strings.Repeat("a", 64)
	prepare := `{"resourceId":"core:inbound:17","endpointId":"tcp:ipv4:1080","factRevision":"` + digest + `","planId":"local-proxy-plan:fixture","planDigest":"` + digest + `","idempotencyKey":"prepare-1","acknowledged":true,"confirmation":"PREPARE LOCAL PROXY local-proxy-plan:fixture"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/local-proxy/prepare", prepare); response.Code != http.StatusOK {
		t.Fatalf("prepare=%d %s", response.Code, response.Body.String())
	}
	apply := `{"operationId":"operation-1","operationRevision":1,"planId":"local-proxy-plan:fixture","planDigest":"` + digest + `","factRevision":"` + digest + `","idempotencyKey":"apply-1","acknowledged":true,"confirmation":"APPLY LOCAL PROXY operation-1"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/local-proxy/apply", apply); response.Code != http.StatusOK {
		t.Fatalf("apply=%d %s", response.Code, response.Body.String())
	}
	rollback := `{"operationId":"operation-1","operationRevision":4,"idempotencyKey":"rollback-1","confirmation":"ROLLBACK LOCAL PROXY operation-1"}`
	if response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/local-proxy/rollback", rollback); response.Code != http.StatusOK {
		t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
	}
	if recorder.prepare.IdempotencyKey != "prepare-1" || recorder.apply.IdempotencyKey != "apply-1" ||
		recorder.disable.Confirmation != "ROLLBACK LOCAL PROXY operation-1" {
		t.Fatalf("semantic request projection failed: %#v %#v %#v", recorder.prepare, recorder.apply, recorder.disable)
	}
}
