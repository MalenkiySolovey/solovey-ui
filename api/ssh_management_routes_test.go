package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostmanagement "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	sshdomain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	sshmanagementservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	"github.com/gin-gonic/gin"
)

func TestSSHManagementRoutesRequireFullBrowserAuthentication(t *testing.T) {
	router, _ := sshManagementTestRouter(t)
	for _, path := range []string{"/api/v1/operations/ssh/current", "/api/v1/operations/ssh/posture", "/api/v1/operations/ssh/capabilities", "/api/v1/operations/ssh/endpoints", "/api/v1/operations/ssh/recovery", "/api/v1/operations/ssh/candidate/ssh-operation:test/timeline"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s unauthenticated status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestRetentionSSHTimelinePagingIsBoundedAndPublishesReplayHorizon(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	valid := sshManagementRequest(router, cookies, http.MethodGet, "/api/v1/operations/ssh/candidate/ssh-operation:test/timeline?after=0&limit=4", "")
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"truncated":false`) || !strings.Contains(valid.Body.String(), `"replayHorizon"`) {
		t.Fatalf("valid page status=%d body=%s", valid.Code, valid.Body.String())
	}
	for _, query := range []string{"limit=0", "limit=129", "limit=invalid", "after=invalid"} {
		response := sshManagementRequest(router, cookies, http.MethodGet, "/api/v1/operations/ssh/candidate/ssh-operation:test/timeline?"+query, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestSSHManagementDTORejectsUnknownAndRawHostFields(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	for _, body := range []string{
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"acknowledged":true,"rawConfig":"PermitRootLogin yes"}`,
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED","path":"/etc/ssh/sshd_config"},"acknowledged":true}`,
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED","port":2222},"acknowledged":true}`,
	} {
		recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/preview", body)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid JSON request") {
			t.Fatalf("unsafe body accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestSSHManagementPreviewIsTruthfulWhenProductionProviderUnavailable(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	body := `{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"acknowledged":true}`
	recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/preview", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response Msg
	if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || !response.Success {
		t.Fatalf("response=%s", recorder.Body.String())
	}
	encoded, _ := json.Marshal(response.Obj)
	text := string(encoded)
	if !strings.Contains(text, `"possible":false`) || !strings.Contains(text, "production_mutation_provider_unavailable") ||
		strings.Contains(text, "/etc/ssh") || strings.Contains(text, "sshd -T") {
		t.Fatalf("preview is not truthful/safe: %s", text)
	}
}

func TestSSHCandidateMutationRequiresStepUpBeforeProviderCall(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	body := `{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"idempotencyKey":"idem:test","expectedPreviewRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedPostureRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedEndpointRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedRecoveryRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedProviderRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","endpointId":"management:ssh:test","principalId":"principal:test","authenticationClass":"publickey","acknowledged":true}`
	recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/candidate", body)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "step-up") {
		t.Fatalf("candidate without step-up status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSSHReconnectConfirmationRejectsRawEvidenceFields(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	for _, body := range []string{
		`{"expectedRevision":1,"providerEvidenceRef":"/etc/ssh/sshd_config"}`,
		`{"expectedRevision":1,"providerEvidenceRef":"ssh-proof:ok","command":"sshd -T"}`,
		`{"expectedRevision":1,"providerEvidenceRef":"ssh-proof:contains space"}`,
	} {
		recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/candidate/ssh-operation:test/reconnect/confirm", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe proof accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestSSHOperationIDsAreTypedAndBounded(t *testing.T) {
	for _, value := range []string{"", "deployment-operation:test", "ssh-operation:contains space", "ssh-operation:../../unsafe", strings.Repeat("a", 65)} {
		if safeSSHOperationID(value) {
			t.Fatalf("unsafe SSH operation ID accepted: %q", value)
		}
	}
	for _, value := range []string{"ssh-operation:test", "ssh-operation:0123456789abcdef"} {
		if !safeSSHOperationID(value) {
			t.Fatalf("valid SSH operation ID rejected: %q", value)
		}
	}
}

func sshManagementTestRouter(t *testing.T, managers ...*sshmanagementservice.Manager) (*gin.Engine, []*http.Cookie) {
	t.Helper()
	initAPITestDB(t, filepath.Join(t.TempDir(), "ssh-management.db"))
	t.Cleanup(func() { closeAPITestDB(t) })
	settings := &service.SettingService{}
	router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
		handler := &APIHandler{ApiService: NewApiService()}
		handler.SSHManagement = sshmanagementservice.Shared()
		if len(managers) > 0 {
			handler.SSHManagement = managers[0]
		}
		handler.registerSSHManagementRoutes(router.Group("/api"))
	})
	return router, cookies
}

func sshManagementRequest(router *gin.Engine, cookies []*http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	for _, item := range cookies {
		request.AddCookie(item)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

type sshCurrentHTTPProvider struct {
	sshmanagementservice.UnavailableProvider
	posture sshdomain.SSHPostureV1
	calls   int
}

func (p *sshCurrentHTTPProvider) Observe(context.Context) (sshmanagementservice.ObservationV1, error) {
	p.calls++
	return sshmanagementservice.ObservationV1{Posture: p.posture, ProviderRevision: sshdomain.Revision("provider")}, nil
}

func TestSSHCurrentHTTPExposesOneCurrentOwnerWithoutWorkflowHistory(t *testing.T) {
	now := time.Now().UTC()
	provider := &sshCurrentHTTPProvider{posture: apiSSHPostureFixture(now)}
	manager := sshmanagementservice.NewManager(sshmanagementservice.Repository{}, provider)
	manager.Now = func() time.Time { return now }
	manager.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 { return nil }
	manager.Evidence = func(context.Context, time.Time) hostmanagement.EvidenceSnapshot {
		return hostmanagement.EvidenceSnapshot{}
	}
	router, cookies := sshManagementTestRouter(t, manager)
	manager.Repository = sshmanagementservice.Shared().Repository
	// Shared repository resolves the current test database at the production seam.
	for _, history := range []bool{false, true} {
		if history {
			old := apiSSHPostureFixture(now.Add(-time.Hour))
			if err := manager.Repository.SavePosture(context.Background(), old, now.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		before := provider.calls
		response := sshManagementRequest(router, cookies, http.MethodGet, "/api/v1/operations/ssh/current", "")
		var message struct {
			Success bool
			Obj     sshmanagementservice.CurrentReadV1
		}
		if err := json.Unmarshal(response.Body.Bytes(), &message); err != nil || !message.Success || !message.Obj.Fresh || message.Obj.Posture == nil || len(message.Obj.Endpoints) != 1 || provider.calls != before+1 {
			t.Fatalf("normal HTTP read lost/split owner: %s", response.Body.String())
		}
		endpoint := message.Obj.Endpoints[0]
		if endpoint.ID != provider.posture.Endpoints[0].ID || endpoint.OwnerRevision != provider.posture.SemanticRevision || endpoint.RuntimeRevision != provider.posture.ListenerAuthorities[0].Revision || endpoint.Owner != sshdomain.ProtectionResourceOwner {
			t.Fatalf("HTTP endpoint binding lost: %#v", endpoint)
		}
		if strings.Contains(response.Body.String(), `"endpoints":null`) || strings.Contains(response.Body.String(), `"paths":null`) {
			t.Fatal("nullable collection")
		}
		latest, err := manager.LatestPosture(context.Background())
		if !history && !errors.Is(err, gorm.ErrRecordNotFound) || history && (err != nil || latest.ObservedAt >= now.Unix()) {
			t.Fatal("HTTP observation mutated history")
		}
	}
	for _, path := range []string{"posture", "endpoints"} {
		response := sshManagementRequest(router, cookies, http.MethodGet, "/api/v1/operations/ssh/"+path, "")
		if !strings.Contains(response.Body.String(), `"success":true`) || path == "posture" && !strings.Contains(response.Body.String(), `"fresh":true`) || path == "endpoints" && !strings.Contains(response.Body.String(), provider.posture.Endpoints[0].ID) {
			t.Fatalf("compatibility read failed: %s", response.Body.String())
		}
	}
}

func apiSSHPostureFixture(now time.Time) sshdomain.SSHPostureV1 {
	configuration := sshdomain.Revision("dropbear-config")
	capabilities := sshdomain.CapabilitySetV1{ObservePosture: sshdomain.AvailabilityAvailable, Prepare: sshdomain.AvailabilityAvailable, Stage: sshdomain.AvailabilityAvailable, Validate: sshdomain.AvailabilityAvailable, Reload: sshdomain.AvailabilityAvailable, Reconnect: sshdomain.AvailabilityAvailable, Rollback: sshdomain.AvailabilityAvailable}
	capabilities.Revision = sshdomain.Revision(capabilities)
	endpoint := hostresources.ManagementEndpointV1{
		Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:configured:ipv4:22:main:0123456789abcdef", Network: hostresources.NetworkTCP,
		Family: hostresources.AddressFamilyIPv4, Bind: "0.0.0.0", Port: 22, ServiceKind: hostresources.ManagementSSH, Exposure: hostresources.EndpointIntentPublic,
		Owner: "system", Purpose: "ssh_administrative_access", RecoveryPolicy: "fresh_independent_path_required", Source: "privileged_broker", ConfiguredIntent: true,
		Wildcard: true, ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(sshdomain.MaxPostureLifetime).Unix(), ConfigurationRevision: configuration, SemanticRevision: configuration,
	}
	binaryRevision, serviceRevision := sshdomain.Revision("dropbear-binary"), sshdomain.Revision("dropbear-service")
	pid, parent, session, root := 933, 1, 933, 0
	authority := sshdomain.SSHListenerAuthorityV1{
		Schema: sshdomain.ListenerAuthoritySchemaV1, EndpointIDs: []string{endpoint.ID}, InstanceID: "instance1",
		Socket:         hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Cookie: 42, Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process:        hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: sshdomain.Revision("process"), PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "123", ExeDigest: binaryRevision, Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &root, GID: &root},
		Service:        hostfacts.ServiceFact{SupervisorRevision: sshdomain.Revision("supervisor"), CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: sshdomain.Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear", ProcdInstance: "instance1", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-P", "/var/run/dropbear.main.pid", "-p", "22"}},
		BinaryRevision: binaryRevision, ServiceRevision: serviceRevision, ConfigurationRevision: configuration, ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	posture := sshdomain.SSHPostureV1{
		Schema:        sshdomain.PostureSchemaV1,
		Binary:        sshdomain.BinaryIdentityV1{Implementation: "dropbear", VersionClass: "2025_88", Digest: binaryRevision, Selected: true},
		Service:       sshdomain.ServiceIdentityV1{Manager: "procd", UnitID: "dropbear", State: "active", Digest: serviceRevision},
		ConfigGraph:   []sshdomain.ConfigNodeV1{{ID: "uci:dropbear", Kind: "uci_package", Order: 0, Depth: 0, Digest: configuration, Owner: "root", ModeClass: "owner_read_write"}},
		MatchContexts: []sshdomain.MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: sshdomain.Revision("effective"), Known: true}},
		Endpoints:     []hostresources.ManagementEndpointV1{endpoint}, ListenerAuthorities: []sshdomain.SSHListenerAuthorityV1{authority},
		Authentication: sshdomain.AuthenticationPostureV1{PasswordAuthentication: "yes", KbdInteractiveAuthentication: "yes", PermitRootLogin: "prohibit-password", PubkeyAuthentication: "yes", AuthenticationMethods: []string{"publickey"}, MaxAuthTries: 6, LoginGraceTimeSeconds: 120, MaxStartupsClass: "bounded_default"},
		Forwarding:     sshdomain.ForwardingPostureV1{AllowAgentForwarding: "yes", AllowTCPForwarding: "yes", GatewayPorts: "no", PermitTunnel: "no", X11Forwarding: "yes"},
		AuthorizedKeys: sshdomain.AuthorizedKeysPostureV1{StrictModes: "yes", PathTemplateCount: 1, PathTemplateRevision: sshdomain.Revision("authorized-key-templates")},
		HostKeys:       []sshdomain.HostKeyPostureV1{{Type: "ed25519", Fingerprint: sshdomain.Revision("host-key"), Count: 1, Owner: "root", ModeClass: "owner_read"}},
		Capabilities:   capabilities, ObservedAt: now.Unix(), ExpiresAt: now.Add(sshdomain.MaxPostureLifetime).Unix(), BinaryRevision: binaryRevision, ServiceRevision: serviceRevision, ConfigurationRevision: configuration,
	}
	posture.SemanticRevision = sshdomain.PostureSemanticRevision(posture)
	return posture
}
