//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrecoverypath "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/recoverypath"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestMVPAPIExactScopesAndRedactedAuditPayload(t *testing.T) {
	for _, scope := range []string{readScope, writeScope, applyScope} {
		router, _ := newProtectionAPIRouter(t, scope)
		response := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/status", "")
		if response.Code != http.StatusOK {
			t.Fatalf("scope %q cannot read component status: %d", scope, response.Code)
		}
	}

	readRouter, _ := newProtectionAPIRouter(t, readScope)
	if response := requestProtectionAPI(readRouter, http.MethodPost, "/api/components/server-protection/firewall/preview", `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("read scope mutated firewall preview: %d", response.Code)
	}
	applyRouter, _, applyManager := newProtectionAPIRouterWithOperations(t, applyScope)
	operation, err := applyManager.Acquire(context.Background(), protectionoperations.AcquireRequest{Kind: protectionoperations.KindFirewall, IdempotencyKey: "scope-apply", Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if response := requestProtectionAPI(applyRouter, http.MethodPost, "/api/components/server-protection/firewall/apply", `{"operationId":"`+operation.Operation.OperationID+`","confirmation":"APPLY SERVER PROTECTION `+operation.Operation.OperationID+`"}`); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "missing_capability") {
		t.Fatalf("apply scope did not reach preview-only capability gate: %d %s", response.Code, response.Body.String())
	}

	writer, audits := newProtectionAPIRouter(t, writeScope)
	secret := "do-not-store-this-allowlist-reason"
	created := requestProtectionAPI(writer, http.MethodPost, "/api/components/server-protection/allowlist/ports", `{"protocol":"tcp","listen":"0.0.0.0","portStart":443,"portEnd":443,"reason":"`+secret+`"}`)
	assertProtectionSuccess(t, created)
	if len(*audits) != 1 || (*audits)[0].Name != "server_protection_port_allowlist_created" {
		t.Fatalf("audit events = %#v", *audits)
	}
	payload, _ := json.Marshal((*audits)[0].Details)
	if strings.Contains(string(payload), secret) || strings.Contains(string(payload), "listen") {
		t.Fatalf("audit payload leaked allowlist input: %s", payload)
	}
}

func TestMVPAPIRejectsBroadScopeAndDangerousSettings(t *testing.T) {
	broad, _ := newProtectionAPIRouter(t, "server-protection")
	response := requestProtectionAPI(broad, http.MethodGet, "/api/components/server-protection/status", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("broad scope response = %d", response.Code)
	}

	writer, _ := newProtectionAPIRouter(t, writeScope)
	settings := domain.DefaultSettings()
	settings.AdvancedAcknowledgedAt = 1
	settings.FeatureFlags["enable_apply_beta"] = true
	payload, _ := json.Marshal(settingsInput{Settings: settings, Revision: 1})
	response = requestProtectionAPI(writer, http.MethodPut, "/api/components/server-protection/settings", string(payload))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "missing_capability") {
		t.Fatalf("dangerous settings response = %d %s", response.Code, response.Body.String())
	}
}

func TestMVPAPIRouteSnapshot(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, "admin")
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/components/server-protection/status",
		"GET /api/components/server-protection/settings",
		"PUT /api/components/server-protection/settings",
		"GET /api/components/server-protection/resources",
		"GET /api/components/server-protection/host-surfaces",
		"GET /api/components/server-protection/target-capabilities",
		"GET /api/components/server-protection/native-fallback/status",
		"GET /api/components/server-protection/udp/status",
		"POST /api/components/server-protection/udp/preview",
		"POST /api/components/server-protection/udp/prepare",
		"POST /api/components/server-protection/udp/apply",
		"POST /api/components/server-protection/udp/rollback",
		"GET /api/components/server-protection/udp/operations/:operationId",
		"GET /api/components/server-protection/udp/operations/:operationId/recovery",
		"GET /api/components/server-protection/local-proxy/status",
		"POST /api/components/server-protection/local-proxy/preview",
		"POST /api/components/server-protection/local-proxy/prepare",
		"POST /api/components/server-protection/local-proxy/apply",
		"POST /api/components/server-protection/local-proxy/disable",
		"POST /api/components/server-protection/local-proxy/rollback",
		"GET /api/components/server-protection/local-proxy/operations/:operationId",
		"GET /api/components/server-protection/local-proxy/operations/:operationId/recovery",
		"GET /api/components/server-protection/interception/status",
		"POST /api/components/server-protection/interception/preview",
		"POST /api/components/server-protection/interception/prepare",
		"POST /api/components/server-protection/interception/apply",
		"POST /api/components/server-protection/interception/disable",
		"POST /api/components/server-protection/interception/rollback",
		"GET /api/components/server-protection/interception/operations/:operationId",
		"GET /api/components/server-protection/interception/operations/:operationId/recovery",
		"POST /api/components/server-protection/native-fallback/preview",
		"POST /api/components/server-protection/native-fallback/prepare",
		"POST /api/components/server-protection/native-fallback/apply",
		"POST /api/components/server-protection/native-fallback/rollback",
		"GET /api/components/server-protection/signals",
		"GET /api/components/server-protection/decisions",
		"GET /api/components/server-protection/posture",
		"GET /api/components/server-protection/firewall-baseline",
		"POST /api/components/server-protection/decisions/resolve-preview",
		"GET /api/components/server-protection/profiles",
		"POST /api/components/server-protection/profiles",
		"PUT /api/components/server-protection/profiles/:id",
		"DELETE /api/components/server-protection/profiles/:id",
		"POST /api/components/server-protection/profiles/:id/reattach",
		"GET /api/components/server-protection/events",
		"DELETE /api/components/server-protection/events",
		"GET /api/components/server-protection/graylist",
		"DELETE /api/components/server-protection/graylist",
		"GET /api/components/server-protection/allowlist/ports",
		"POST /api/components/server-protection/allowlist/ports",
		"DELETE /api/components/server-protection/allowlist/ports/:id",
		"GET /api/components/server-protection/allowlist/ips",
		"POST /api/components/server-protection/allowlist/ips",
		"DELETE /api/components/server-protection/allowlist/ips/:id",
		"GET /api/components/server-protection/diagnostics",
		"POST /api/components/server-protection/firewall/preview",
		"GET /api/components/server-protection/fronting/status",
		"POST /api/components/server-protection/fronting/preview",
		"POST /api/components/server-protection/fronting/sync",
		"POST /api/components/server-protection/fronting/apply",
		"POST /api/components/server-protection/fronting/rollback",
		"GET /api/components/server-protection/fronting/operations/:operationId",
		"POST /api/components/server-protection/firewall/prepare",
		"GET /api/components/server-protection/operations",
		"POST /api/components/server-protection/operations/:operationId/force-unlock",
		"POST /api/components/server-protection/operations/:operationId/forget-state",
		"POST /api/components/server-protection/firewall/apply",
		"POST /api/components/server-protection/firewall/rollback",
		"POST /api/components/server-protection/ports/prepare",
		"POST /api/components/server-protection/ports/apply",
		"POST /api/components/server-protection/ports/rollback",
	} {
		if !routes[route] {
			t.Fatalf("missing component route %s", route)
		}
	}
}

func TestContractInspectionAPIsArePaginatedReadOnlyAndHonest(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, readScope)
	for _, path := range []string{"/api/components/server-protection/resources?limit=1", "/api/components/server-protection/host-surfaces?limit=1&refresh=true", "/api/components/server-protection/target-capabilities?limit=1", "/api/components/server-protection/signals?limit=1", "/api/components/server-protection/decisions?limit=1", "/api/components/server-protection/posture?limit=1"} {
		response := requestProtectionAPI(router, http.MethodGet, path, "")
		envelope := assertProtectionSuccess(t, response)
		if !strings.Contains(string(envelope.Obj), "\"limit\":1") && strings.Contains(path, "posture") == false {
			t.Fatalf("route %s omitted pagination: %s", path, response.Body.String())
		}
	}
	decisionResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/decisions", "")
	if !strings.Contains(decisionResponse.Body.String(), `"actionability":"resolver_preview_only"`) || !strings.Contains(decisionResponse.Body.String(), `"actualStatus":"NOT_APPLIED"`) || strings.Contains(decisionResponse.Body.String(), `"state":"APPLIED"`) {
		t.Fatalf("decision API false-claimed applied action: %s", decisionResponse.Body.String())
	}
	hostResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/host-surfaces?refresh=true", "")
	if !strings.Contains(hostResponse.Body.String(), "UNKNOWN_OWNER") {
		t.Fatalf("absent host provider did not yield explicit unknown: %s", hostResponse.Body.String())
	}
	posture := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/posture", "")
	if !strings.Contains(posture.Body.String(), "recovery_path_unproven") || !strings.Contains(posture.Body.String(), "contract_only_no_observed_endpoint") {
		t.Fatalf("posture overclaimed recovery: %s", posture.Body.String())
	}
	firewallBaseline := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/firewall-baseline", "")
	if firewallBaseline.Code != http.StatusOK || !strings.Contains(firewallBaseline.Body.String(), `"realNftablesLive":"NOT_RUN"`) || !strings.Contains(firewallBaseline.Body.String(), `"actual":"NOT_APPLIED"`) || !strings.Contains(firewallBaseline.Body.String(), `"socketGraph"`) {
		t.Fatalf("firewall baseline preview omitted honest graph/kernel state: %d %s", firewallBaseline.Code, firewallBaseline.Body.String())
	}
	overflow := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/host-surfaces?page=9223372036854775807&limit=500", "")
	if overflow.Code != http.StatusOK {
		t.Fatalf("overflowing page crashed read-only API: %d %s", overflow.Code, overflow.Body.String())
	}
}

func TestContractInspectionRejectsCorruptAppliedAndCrossScopeRows(t *testing.T) {
	router, _, _, _, db := newProtectionAPIRouterWithDB(t, readScope, protectionfronting.NewNginxAdapter())
	now := time.Now().UTC().Truncate(time.Second)
	decision := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, DecisionID: strings.Repeat("a", 64), PolicyRevision: "policy-1", Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:1"}, TargetResourceIDs: []string{"core:inbound:1"}, SourceClasses: []string{"native"}, ScoreSnapshot: domain.ScoreSnapshotV2{Score: 10, TargetGroup: "core:inbound:1", CapturedAt: now}, ReasonCodes: []string{domain.ReasonCapabilityUnavailable}, RequestedIntent: domain.IntentTemporaryBlock, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: domain.PolicyCheckV2{Result: "unknown"}, RecoveryResult: domain.PolicyCheckV2{Result: "unknown"}, CapabilityResolution: domain.CapabilityResolutionV2{ResolvedIntent: domain.IntentObserve}, State: domain.DecisionApplied}
	decisionJSON, _ := json.Marshal(decision)
	if err := db.Create(&protectionrepository.ProtectionDecisionV2Model{DecisionID: decision.DecisionID, Schema: decision.Schema, PolicyRevision: decision.PolicyRevision, SubjectType: decision.Subject.Type, SubjectValue: decision.Subject.Value, Scope: string(decision.Scope.Scope), RequestedIntent: string(decision.RequestedIntent), ResolvedIntent: string(decision.CapabilityResolution.ResolvedIntent), State: string(decision.State), CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(), ContractJSON: decisionJSON}).Error; err != nil {
		t.Fatal(err)
	}
	signal := domain.ProtectionSignalV2{Schema: domain.ProtectionSignalSchemaV2, SignalID: strings.Repeat("b", 64), Source: domain.SignalSourceV2{SourceID: "fixture", Producer: "fixture", ProducerVersion: "v1", TrustClass: "native", SourceClass: "native"}, Category: domain.SignalCategoryPanelAuth, Kind: "failed_login", Subject: domain.SignalSubjectV2{Type: "account_pseudonym", Value: "account:" + strings.Repeat("c", 64)}, Scope: domain.SignalScopeV2{Scope: domain.ScopeHostWide}, ObservedAt: now, ExpiresAt: now.Add(time.Hour), ConfidenceBP: 5000, Provenance: domain.SignalProvenanceV2{AdapterID: "fixture", SourceRevision: "source-1", PolicyRevision: "policy-1"}}
	signalJSON, _ := json.Marshal(signal)
	if err := db.Create(&protectionrepository.ProtectionSignalV2Model{SignalID: signal.SignalID, Schema: signal.Schema, SourceID: signal.Source.SourceID, SourceClass: signal.Source.SourceClass, Category: string(signal.Category), Kind: signal.Kind, SubjectType: signal.Subject.Type, SubjectValue: signal.Subject.Value, Scope: string(signal.Scope.Scope), ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(), ConfidenceBP: signal.ConfidenceBP, PolicyRevision: signal.Provenance.PolicyRevision, ContractJSON: signalJSON}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&protectionrepository.FallbackTargetLeaseModel{LeaseID: "fallback-lease:bad", HolderID: `/var/lib/private/token`, ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(), State: "ACTIVE", ReasonCodesJSON: []byte(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&protectionrepository.RecoveryPathModel{RecoveryPathID: "recovery:bad", Kind: string(hostresources.ManagementPanel), EndpointID: `/var/lib/private/panel`, PrincipalID: "principal:hash", VerificationMethod: "fresh_panel_login", VerifiedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: []byte(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/components/server-protection/decisions", "/api/components/server-protection/signals"} {
		response := requestProtectionAPI(router, http.MethodGet, path, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"invalidRecords":1`) || strings.Contains(response.Body.String(), `"state":"APPLIED"`) || strings.Contains(response.Body.String(), `"scope":"HOST_WIDE"`) {
			t.Fatalf("invalid contract escaped %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	leaseResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/target-capabilities", "")
	if !strings.Contains(leaseResponse.Body.String(), `"invalidLeaseRecords":1`) || strings.Contains(leaseResponse.Body.String(), "/var/lib/private") {
		t.Fatalf("invalid lease leaked through API: %s", leaseResponse.Body.String())
	}
	postureResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/posture", "")
	if !strings.Contains(postureResponse.Body.String(), `"invalidRecoveryRecords":0`) || strings.Contains(postureResponse.Body.String(), "/var/lib/private") {
		t.Fatalf("invalid recovery path leaked through API: %s", postureResponse.Body.String())
	}
}

func TestFirewallBaselineResolverTargetingDoesNotRequireListenerOwner(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, writeScope)
	now := time.Now().UTC().Truncate(time.Second)
	decision := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: "endpoint-baseline-policy", Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "fixture:listener:one"}, TargetResourceIDs: []string{"fixture:listener:one"}, SignalRefs: []string{strings.Repeat("d", 64)}, SourceClasses: []string{"native"}, ScoreSnapshot: domain.ScoreSnapshotV2{Score: 100, TargetGroup: "fixture:listener:one", CapturedAt: now}, ConfidenceBP: 9000, RequestedIntent: domain.IntentTemporaryBlock, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: domain.PolicyCheckV2{Result: "not_evaluated"}, RecoveryResult: domain.PolicyCheckV2{Result: "not_evaluated"}, CapabilityResolution: domain.CapabilityResolutionV2{ResolvedIntent: domain.IntentObserve}, State: domain.DecisionCandidate}
	decision.FinalizeID()
	baselineResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/firewall-baseline?refresh=true", "")
	var baseline struct {
		SnapshotBinding firewallBaselineSnapshotBinding `json:"snapshotBinding"`
	}
	decodeProtectionObject(t, baselineResponse, &baseline)
	payload, _ := json.Marshal(resolvePreviewInput{Decision: decision, ExpectedBindingRevision: baseline.SnapshotBinding.Revision})
	response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/decisions/resolve-preview", string(payload))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"selectedIntent":"OBSERVE"`) || !strings.Contains(response.Body.String(), `"actual":"NOT_APPLIED"`) || strings.Contains(response.Body.String(), `"state":"APPLIED"`) {
		t.Fatalf("resolver preview incorrectly required listener ownership or claimed apply: %d %s", response.Code, response.Body.String())
	}
}

func TestRecoveryStateRequiresFreshProofForExactCurrentEndpoint(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	endpoint := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel:web", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Port: 443, ServiceKind: hostresources.ManagementPanel, ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("c", 64)}
	path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:one", Kind: string(hostresources.ManagementPanel), EndpointID: endpoint.ID, PrincipalID: "principal:hash", VerificationMethod: "fresh_panel_login", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: endpoint.ConfigurationRevision}
	if got := recoveryState([]hostresources.RecoveryPathV1{path}, []hostresources.ManagementEndpointV1{endpoint}, now); got != "fresh_independent_path_present" {
		t.Fatalf("exact fresh recovery proof rejected: %s", got)
	}
	path.EndpointID = "management:removed"
	if got := recoveryState([]hostresources.RecoveryPathV1{path}, []hostresources.ManagementEndpointV1{endpoint}, now); got != "recovery_path_unproven" {
		t.Fatalf("unrelated recovery endpoint accepted: %s", got)
	}
	path.EndpointID = endpoint.ID
	endpoint.ReasonCodes = []string{"stale"}
	if got := recoveryState([]hostresources.RecoveryPathV1{path}, []hostresources.ManagementEndpointV1{endpoint}, now); got != "recovery_path_unproven" {
		t.Fatalf("stale management endpoint accepted: %s", got)
	}
}

func TestSSHIdentificationUsesExactUnitOrResourceIdentity(t *testing.T) {
	if !protectionrecoverypath.IsSSHSurface(hostfacts.HostSurfaceFactV1{Service: hostfacts.ServiceFact{SystemdUnit: "sshd.service"}}) ||
		!protectionrecoverypath.IsSSHSurface(hostfacts.HostSurfaceFactV1{Service: hostfacts.ServiceFact{SystemdUnit: "sshd@tenant.service"}}) ||
		!protectionrecoverypath.IsSSHSurface(hostfacts.HostSurfaceFactV1{RegisteredResourceID: "core:ssh:primary"}) {
		t.Fatal("exact SSH identity was rejected")
	}
	for _, value := range []string{"not-sshd.service", "backup-ssh-agent.service", "ssh.service.evil"} {
		if protectionrecoverypath.IsSSHSurface(hostfacts.HostSurfaceFactV1{Service: hostfacts.ServiceFact{SystemdUnit: value}}) {
			t.Fatalf("substring-only SSH identity accepted: %q", value)
		}
	}
}
