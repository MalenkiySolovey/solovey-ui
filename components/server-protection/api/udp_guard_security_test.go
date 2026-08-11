//go:build !minimal

package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestUDPGuardRoutesUseExactScopesAndStrictSafeErrors(t *testing.T) {
	reader, _ := newProtectionAPIRouter(t, readScope)
	if response := requestProtectionAPI(reader, http.MethodPost, "/api/components/server-protection/udp/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read scope reached UDP write route: %d %s", response.Code, response.Body.String())
	}
	writer, _ := newProtectionAPIRouter(t, writeScope)
	unknown := requestProtectionAPI(writer, http.MethodPost, "/api/components/server-protection/udp/preview", `{"planId":"udp-plan:test","unknown":"no"}`)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "malformed_input") {
		t.Fatalf("unknown field response=%d %s", unknown.Code, unknown.Body.String())
	}
	wrongType := requestProtectionAPI(writer, http.MethodPost, "/api/components/server-protection/udp/preview", `{"planId":7}`)
	if wrongType.Code != http.StatusBadRequest || strings.Contains(wrongType.Body.String(), "cannot unmarshal") {
		t.Fatalf("type error leaked details=%d %s", wrongType.Code, wrongType.Body.String())
	}
	apply, _ := newProtectionAPIRouter(t, applyScope)
	confirmation := requestProtectionAPI(apply, http.MethodPost, "/api/components/server-protection/udp/apply", `{"planId":"udp-plan:test","operationId":"operation:test","operationRevision":1,"idempotencyKey":"key","experimentalRiskAcknowledged":true,"confirmation":"wrong"}`)
	if confirmation.Code != http.StatusBadRequest || !strings.Contains(confirmation.Body.String(), "CONFIRMATION_REQUIRED") || strings.Contains(confirmation.Body.String(), "wrong") {
		t.Fatalf("confirmation response=%d %s", confirmation.Code, confirmation.Body.String())
	}
}

func TestUDPGuardRequestContractRejectsCallerSocketAndProtocolData(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, writeScope)
	for _, field := range []string{"bind", "port", "ip", "rule", "table", "timeout", "payload", "sni", "cid", "dns", "credentials", "tls"} {
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/udp/preview", `{"planId":"udp-plan:test","`+field+`":"forbidden"}`)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
			t.Fatalf("field %s escaped strict decoder: %d %s", field, response.Code, response.Body.String())
		}
	}
}
