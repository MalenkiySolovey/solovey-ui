//go:build !minimal

package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestOperationAPIRequiresExplicitForceUnlockConfirmation(t *testing.T) {
	router, audits, manager := newProtectionAPIRouterWithOperations(t, applyScope)
	port := 443
	acquired, err := manager.Acquire(context.Background(), protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindFirewall, ResourceID: "panel:https", Protocol: "tcp",
		Listen: "0.0.0.0", Port: &port, IdempotencyKey: "api-force-unlock", Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/components/server-protection/operations/" + acquired.Operation.OperationID + "/force-unlock"
	denied := requestProtectionAPI(router, http.MethodPost, path, `{"revision":1,"confirmation":"wrong"}`)
	if denied.Code != http.StatusBadRequest || !strings.Contains(denied.Body.String(), "confirmation_required") {
		t.Fatalf("unconfirmed force unlock = %d %s", denied.Code, denied.Body.String())
	}
	if len(*audits) != 0 {
		t.Fatalf("unconfirmed force unlock was audited as accepted: %#v", *audits)
	}
	accepted := requestProtectionAPI(router, http.MethodPost, path, `{"revision":1,"confirmation":"FORCE UNLOCK `+acquired.Operation.OperationID+`"}`)
	if accepted.Code != http.StatusOK {
		t.Fatalf("confirmed force unlock = %d %s", accepted.Code, accepted.Body.String())
	}
	if len(*audits) != 1 || (*audits)[0].Name != "server_protection.force_unlock" {
		t.Fatalf("force unlock audits = %#v", *audits)
	}
}

func TestRetainedOperationDoesNotGrantRollbackAuthorization(t *testing.T) {
	for _, scope := range []string{readScope, writeScope, applyScope} {
		t.Run(scope, func(t *testing.T) {
			router, _, manager := newProtectionAPIRouterWithOperations(t, scope)
			acquired, err := manager.Acquire(t.Context(), protectionoperations.AcquireRequest{Kind: protectionoperations.KindFirewall, ResourceID: "managed-table:inet:solovey_protection", IdempotencyKey: "retained-auth", Actor: "tester"})
			if err != nil {
				t.Fatal(err)
			}
			retained, err := manager.Transition(t.Context(), acquired.Operation.OperationID, acquired.Operation.Revision, protectionoperations.StateReconcileRequired)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/components/server-protection/firewall/rollback"
			body := `{"operationId":"` + retained.OperationID + `","confirmation":"ROLLBACK SERVER PROTECTION ` + retained.OperationID + `"}`
			response := requestProtectionAPI(router, http.MethodPost, path, body)
			if scope != applyScope {
				if response.Code != http.StatusForbidden {
					t.Fatalf("scope authorized retained rollback: %d", response.Code)
				}
			} else {
				if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "missing_capability") {
					t.Fatal("retained operation bypassed advanced policy")
				}
				wrong := requestProtectionAPI(router, http.MethodPost, path, `{"operationId":"`+retained.OperationID+`","confirmation":"wrong"}`)
				if wrong.Code != http.StatusBadRequest || !strings.Contains(wrong.Body.String(), "confirmation_required") {
					t.Fatal("retained operation bypassed confirmation")
				}
			}
			items, err := manager.List(t.Context())
			if err != nil || len(items) != 1 || items[0].State != retained.State || items[0].Revision != retained.Revision {
				t.Fatal("rejected route changed durable operation")
			}
		})
	}
}

func TestOperationAPIConfirmationAndAuditChoreography(t *testing.T) {
	router, audits, manager := newProtectionAPIRouterWithOperations(t, applyScope)
	prepared, err := manager.Acquire(context.Background(), protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindFirewall, ResourceID: "panel:https", Protocol: "tcp",
		IdempotencyKey: "choreography", Actor: "tester", InitialState: protectionoperations.StatePrepared,
	})
	if err != nil {
		t.Fatal(err)
	}
	operationID := prepared.Operation.OperationID
	operationsResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/operations", "")
	var operationsState struct {
		Items                 []operationView   `json:"items"`
		ConfirmationTemplates map[string]string `json:"confirmationTemplates"`
	}
	decodeProtectionObject(t, operationsResponse, &operationsState)
	if len(operationsState.Items) != 1 || operationsState.Items[0].OperationID != operationID || operationsState.ConfirmationTemplates["forgetState"] != "FORGET_SERVER_PROTECTION_STATE" {
		t.Fatalf("recovery API state = %#v", operationsState)
	}
	wrongApply := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/apply", `{"operationId":"`+operationID+`","confirmation":"wrong"}`)
	if wrongApply.Code != http.StatusBadRequest || len(*audits) != 0 {
		t.Fatalf("unconfirmed apply=%d audits=%#v", wrongApply.Code, *audits)
	}
	apply := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/apply", `{"operationId":"`+operationID+`","confirmation":"APPLY SERVER PROTECTION `+operationID+`"}`)
	rollback := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/rollback", `{"operationId":"`+operationID+`","confirmation":"ROLLBACK SERVER PROTECTION `+operationID+`"}`)
	if apply.Code != http.StatusConflict || rollback.Code != http.StatusConflict {
		t.Fatalf("capability gates apply=%d rollback=%d", apply.Code, rollback.Code)
	}
	applying, err := manager.Transition(context.Background(), operationID, prepared.Operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := manager.Transition(context.Background(), operationID, applying.Revision, protectionoperations.StateApplied)
	if err != nil {
		t.Fatal(err)
	}
	forgetPath := "/api/components/server-protection/operations/" + operationID + "/forget-state"
	forgotten := requestProtectionAPI(router, http.MethodPost, forgetPath, `{"revision":`+strconv.Itoa(applied.Revision)+`,"confirmation":"FORGET_SERVER_PROTECTION_STATE"}`)
	if forgotten.Code != http.StatusOK {
		t.Fatalf("forget state=%d %s", forgotten.Code, forgotten.Body.String())
	}
	names := make([]string, 0, len(*audits))
	for _, audit := range *audits {
		names = append(names, audit.Name)
	}
	for _, expected := range []string{"server_protection.apply", "server_protection.rollback", "server_protection.forget_state"} {
		if !slices.Contains(names, expected) {
			t.Fatalf("missing audit %s in %#v", expected, names)
		}
	}
}
