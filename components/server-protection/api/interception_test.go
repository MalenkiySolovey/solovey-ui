//go:build !minimal

package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestInterceptionRoutesAreScopedAndStatusIsHonest(t *testing.T) {
	reader, _ := newProtectionAPIRouter(t, readScope)
	status := requestProtectionAPI(reader, http.MethodGet, "/api/components/server-protection/interception/status", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"mutationAvailable":false`) ||
		!strings.Contains(status.Body.String(), `"defaultEnabled":false`) ||
		!strings.Contains(status.Body.String(), `"localOutputShipped":false`) ||
		!strings.Contains(status.Body.String(), `"tunAdoptionShipped":false`) {
		t.Fatalf("dishonest interception status: %d %s", status.Code, status.Body.String())
	}
	if response := requestProtectionAPI(reader, http.MethodPost, "/api/components/server-protection/interception/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read scope reached preview: %d %s", response.Code, response.Body.String())
	}
}

func TestInterceptionAPIRejectsCallerNetworkAuthority(t *testing.T) {
	writer, _ := newProtectionAPIRouter(t, writeScope)
	for _, field := range []string{
		"interfaceName", "interfaceIndex", "mark", "mask", "routingTable", "priority", "route",
		"cidr", "port", "nftables", "ipCommand", "chain", "table", "source", "destination",
		"process", "user", "cgroup", "namespace", "dockerContainer", "tun",
	} {
		body := `{"interception":{},"` + field + `":"forbidden"}`
		response := requestProtectionAPI(writer, http.MethodPost, "/api/components/server-protection/interception/preview", body)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "malformed_input") {
			t.Fatalf("field %q escaped strict decoder: %d %s", field, response.Code, response.Body.String())
		}
	}
}

func TestInterceptionMutationEndpointsRemainExplicitlyNotShipped(t *testing.T) {
	router, audits, _, _, _ := newProtectionAPIRouterWithDB(t, applyScope, nil)
	digest := strings.Repeat("a", 64)
	body := `{"planId":"interception-plan:fixture","expectedRevision":"` + digest +
		`","idempotencyKey":"mutation-1","confirmation":"I UNDERSTAND FORWARDED INTERCEPTION IS NOT SHIPPED"}`
	for _, action := range []string{"prepare", "apply", "disable", "rollback"} {
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/interception/"+action, body)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "INTERCEPTION_MUTATION_NOT_SHIPPED") {
			t.Fatalf("%s mutation response: %d %s", action, response.Code, response.Body.String())
		}
	}
	if len(*audits) != 4 {
		t.Fatalf("rejected mutations were not safely audited: %#v", *audits)
	}
}
