package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostmanagement "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhealth "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/health"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionhostsurface "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/hostsurface"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	sshmanagement "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	sshservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
	"gorm.io/gorm"
)

type chainSSHPostureReader struct{ posture sshmanagement.SSHPostureV1 }

type controlledBootGeneration struct{ generation string }

func (b *controlledBootGeneration) Generation(context.Context) (string, error) {
	return b.generation, nil
}
func (*controlledBootGeneration) RequiresBaseFirewall() bool { return false }

func TestCommittedBootRecoveryProductionManagementLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "waiting", "stale_management", "integrity", "same_boot", "validation_failure", "apply_failure", "health_failure", "foreign", "interrupted_before_apply", "interrupted_after_apply"} {
		t.Run(scenario, func(t *testing.T) { assertCurrentSSHProductionLifecycle(t, true, "", scenario) })
	}
}

type stoppedRuntimePID struct{}

func (stoppedRuntimePID) Alive(int) (bool, error) { return false, nil }

type interruptedRuntimeStore struct {
	RuntimeStore
	beforeApply bool
	fired       bool
}

func (s *interruptedRuntimeStore) UpdateFirewallRuntime(ctx context.Context, id string, revision int, composition string, before, after protectionrepository.FirewallRuntimeBinding) error {
	if !s.fired && ((s.beforeApply && after.State == "RESTORING_RUNTIME") || (!s.beforeApply && before.State == "RESTORING_RUNTIME" && after.State == "HEALTH_VERIFIED")) {
		s.fired = true
		if s.beforeApply {
			if err := s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after); err != nil {
				return err
			}
		}
		return errors.New("interrupted durable write")
	}
	return s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after)
}

type mismatchedRuntimeStore struct{ FirewallContributionStore }

func (s mismatchedRuntimeStore) FirewallAuthority(ctx context.Context) (protectionrepository.FirewallAuthoritySnapshot, error) {
	snapshot, err := s.FirewallContributionStore.FirewallAuthority(ctx)
	if snapshot.HasComposition {
		snapshot.Composition.CandidateSemanticSHA256 = hostresources.Revision("mismatched")
	}
	return snapshot, err
}

func (r chainSSHPostureReader) CurrentPosture(context.Context) (*sshmanagement.SSHPostureV1, error) {
	copy := r.posture
	return &copy, nil
}

func TestStockDropbearAuthorityReachesPreparedTypedFirewallCandidate(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	posture := stockDropbearPostureFixture(now)
	assertSSHPostureReachesPreparedTypedFirewallCandidate(t, now, posture)
}

func assertSSHPostureReachesPreparedTypedFirewallCandidate(t *testing.T, now time.Time, posture sshmanagement.SSHPostureV1) {
	t.Helper()
	sshResources, err := sshmanagement.ProtectableResources(posture, now)
	if err != nil || len(sshResources) != 1 {
		t.Fatalf("SSH resource projection failed: resources=%#v err=%v", sshResources, err)
	}
	projection, err := (protectionhostsurface.SSHPostureProjectionSource{Reader: chainSSHPostureReader{posture: posture}}).ProjectSSHListeners(context.Background(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	panel := hostresources.ProtectableResource{
		ID: "core:panel:web", Kind: "panel_web", Owner: "solovey-ui", Name: "Panel", Protocol: "stream", Listen: "0.0.0.0", Port: 2095, Public: true, Source: "core",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: sshmanagement.Revision("panel-owner"), ConfigRevision: sshmanagement.Revision("panel-config")},
	}
	panel.ListenIntent = hostresources.BuildConfiguredListenIntent(panel)
	resources := append([]hostresources.ProtectableResource{panel}, sshResources...)
	authority := posture.ListenerAuthorities[0]
	observed := protectionhostsurface.NormalizeWithOwnersAndSSH(
		protectionhostsurface.PlatformSnapshot{Sockets: []protectionhostsurface.RawSocket{{Network: authority.Socket.Network, Family: authority.Socket.Family, Bind: authority.Socket.Bind, Port: authority.Socket.Port, Protocol: "tcp", Inode: authority.Socket.Inode}}},
		hostresources.ResourceSnapshot{GeneratedAt: now.Unix(), Resources: resources}, nil, projection, "", now,
	)
	sshSurfaceCount, unobservedSSHCount := 0, 0
	for _, surface := range observed.Facts {
		if surface.RegisteredResourceID != sshResources[0].ID {
			continue
		}
		if surface.SemanticOwner != nil {
			sshSurfaceCount++
		}
		if surface.Classification == hostfacts.ClassificationUnobserved {
			unobservedSSHCount++
		}
	}
	if sshSurfaceCount != 1 || unobservedSSHCount != 0 {
		t.Fatalf("exact SSH HostSurface projection failed: %#v", observed)
	}
	management := hostmanagement.Endpoints(resources, hostfacts.Snapshot{GeneratedAt: now.Unix(), Facts: observed.Facts}, now)
	var panelEndpoint hostresources.ManagementEndpointV1
	sshPresent := false
	for _, endpoint := range management {
		if endpoint.ServiceKind == hostresources.ManagementPanel {
			panelEndpoint = endpoint
		}
		if endpoint.ServiceKind == hostresources.ManagementSSH && endpoint.ResourceID == sshResources[0].ID && endpoint.Port == authority.Socket.Port {
			sshPresent = true
		}
	}
	if panelEndpoint.ID == "" || !sshPresent {
		t.Fatalf("management inventory lost panel or SSH: %#v", management)
	}
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: resources, Surfaces: observed.Facts, Now: now})
	graphEvidence, err := protectionresources.BuildSocketOwnershipGraphEvidence(graph, resources, observed.Facts, now)
	if err != nil || graphEvidence.Revision == "" {
		t.Fatalf("SSH graph evidence could not be frozen: evidence=%#v err=%v", graphEvidence, err)
	}
	recovery := hostresources.RecoveryPathV1{
		Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:panel-login", Kind: string(hostresources.ManagementPanel), EndpointID: panelEndpoint.ID,
		PrincipalID: "principal:fixture", SourcePrefix: "198.51.100.0/24", VerificationMethod: "fresh_panel_login",
		VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(), IndependenceClass: "independent_reconnect",
		VerificationState: "verified", SourceRevision: sshmanagement.Revision("panel-login"), ConfigurationRevision: panelEndpoint.ConfigurationRevision,
	}
	plan := BuildEndpointPlan(EndpointPlanInput{
		Graph: graph, Resources: resources, Management: management, RecoveryPaths: []hostresources.RecoveryPathV1{recovery},
		TrustedSources: []string{"198.51.100.0/24"}, RequireSSHKeep: true, Now: now,
	})
	if err := Preflight(plan); err != nil || !plan.BaselineEligibility.MutationReady || !slices.Contains(plan.AllowTCPPorts, int(authority.Socket.Port)) {
		t.Fatalf("SSH management-preservation candidate failed: plan=%#v err=%v", plan, err)
	}
	preview := Preview(plan, PreviewOptions{IncludeGeneratedNFT: true, NFTCapability: NFTPreviewCapability{Available: true, Revision: sshmanagement.Revision("nft-capability")}})
	if preview.Backend != "preview_only" || preview.Revision != plan.Revision || preview.GeneratedNFT == "" {
		t.Fatalf("SSH candidate preview is incomplete: %#v", preview)
	}
	candidate := RenderManagedNFT(plan)
	if !strings.Contains(candidate, "solovey-revision:"+plan.Revision) || !strings.Contains(candidate, "dport "+strconv.Itoa(int(authority.Socket.Port))) || strings.Contains(candidate, "flush ruleset") || strings.Contains(candidate, "table inet fw4") {
		t.Fatalf("SSH candidate crossed its managed-table boundary:\n%s", candidate)
	}
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	workflow.Now = func() time.Time { return now }
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "stock-dropbear-chain", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatalf("prepared management-preservation state failed: %v", err)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
		t.Fatal("Prepare performed firewall mutation")
	}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil || result.ActualStatus != "APPLIED" {
		t.Fatalf("typed helper authorization rejected the exact candidate: result=%#v err=%v", result, err)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTValidate) == 0 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) == 0 {
		t.Fatalf("candidate did not reach the typed helper boundary: %#v", helper.Requests)
	}
}

func stockDropbearPostureFixture(now time.Time) sshmanagement.SSHPostureV1 {
	configuration := sshmanagement.Revision("dropbear-config")
	capabilities := sshmanagement.CapabilitySetV1{ObservePosture: sshmanagement.AvailabilityAvailable, Prepare: sshmanagement.AvailabilityAvailable, Stage: sshmanagement.AvailabilityAvailable, Validate: sshmanagement.AvailabilityAvailable, Reload: sshmanagement.AvailabilityAvailable, Reconnect: sshmanagement.AvailabilityAvailable, Rollback: sshmanagement.AvailabilityAvailable}
	capabilities.Revision = sshmanagement.Revision(capabilities)
	endpoint := hostresources.ManagementEndpointV1{
		Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:configured:ipv4:22:main:0123456789abcdef", Network: hostresources.NetworkTCP,
		Family: hostresources.AddressFamilyIPv4, Bind: "0.0.0.0", Port: 22, ServiceKind: hostresources.ManagementSSH, Exposure: hostresources.EndpointIntentPublic,
		Owner: "system", Purpose: "ssh_administrative_access", RecoveryPolicy: "fresh_independent_path_required", Source: "privileged_broker", ConfiguredIntent: true,
		Wildcard: true, ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(sshmanagement.MaxPostureLifetime).Unix(), ConfigurationRevision: configuration, SemanticRevision: configuration,
	}
	binaryRevision, serviceRevision := sshmanagement.Revision("dropbear-binary"), sshmanagement.Revision("dropbear-service")
	pid, parent, session, root := 933, 1, 933, 0
	authority := sshmanagement.SSHListenerAuthorityV1{
		Schema: sshmanagement.ListenerAuthoritySchemaV1, EndpointIDs: []string{endpoint.ID}, InstanceID: "instance1",
		Socket:         hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Cookie: 42, Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process:        hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: sshmanagement.Revision("process"), PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "123", ExeDigest: binaryRevision, Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &root, GID: &root},
		Service:        hostfacts.ServiceFact{SupervisorRevision: sshmanagement.Revision("supervisor"), CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: sshmanagement.Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear", ProcdInstance: "instance1", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-P", "/var/run/dropbear.main.pid", "-p", "22"}},
		BinaryRevision: binaryRevision, ServiceRevision: serviceRevision, ConfigurationRevision: configuration, ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	posture := sshmanagement.SSHPostureV1{
		Schema:        sshmanagement.PostureSchemaV1,
		Binary:        sshmanagement.BinaryIdentityV1{Implementation: "dropbear", VersionClass: "2025_88", Digest: binaryRevision, Selected: true},
		Service:       sshmanagement.ServiceIdentityV1{Manager: "procd", UnitID: "dropbear", State: "active", Digest: serviceRevision},
		ConfigGraph:   []sshmanagement.ConfigNodeV1{{ID: "uci:dropbear", Kind: "uci_package", Order: 0, Depth: 0, Digest: configuration, Owner: "root", ModeClass: "owner_read_write"}},
		MatchContexts: []sshmanagement.MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: sshmanagement.Revision("effective"), Known: true}},
		Endpoints:     []hostresources.ManagementEndpointV1{endpoint}, ListenerAuthorities: []sshmanagement.SSHListenerAuthorityV1{authority},
		Authentication: sshmanagement.AuthenticationPostureV1{PasswordAuthentication: "yes", KbdInteractiveAuthentication: "yes", PermitRootLogin: "prohibit-password", PubkeyAuthentication: "yes", AuthenticationMethods: []string{"publickey"}, MaxAuthTries: 6, LoginGraceTimeSeconds: 120, MaxStartupsClass: "bounded_default"},
		Forwarding:     sshmanagement.ForwardingPostureV1{AllowAgentForwarding: "yes", AllowTCPForwarding: "yes", GatewayPorts: "no", PermitTunnel: "no", X11Forwarding: "yes"},
		AuthorizedKeys: sshmanagement.AuthorizedKeysPostureV1{StrictModes: "yes", PathTemplateCount: 1, PathTemplateRevision: sshmanagement.Revision("authorized-key-templates")},
		HostKeys:       []sshmanagement.HostKeyPostureV1{{Type: "ed25519", Fingerprint: sshmanagement.Revision("host-key"), Count: 1, Owner: "root", ModeClass: "owner_read"}},
		Capabilities:   capabilities, ObservedAt: now.Unix(), ExpiresAt: now.Add(sshmanagement.MaxPostureLifetime).Unix(), BinaryRevision: binaryRevision, ServiceRevision: serviceRevision, ConfigurationRevision: configuration,
	}
	posture.SemanticRevision = sshmanagement.PostureSemanticRevision(posture)
	return posture
}

// The observation seam is injected; every downstream projection is production.
type currentChainProvider struct {
	sshservice.UnavailableProvider
	posture  sshmanagement.SSHPostureV1
	observe  func()
	calls    int
	recovery []hostresources.RecoveryPathV1
}

func (p *currentChainProvider) ProviderID() string { return "current-chain-recovery" }
func (p *currentChainProvider) RecoveryPaths(context.Context, time.Time) ([]hostresources.RecoveryPathV1, error) {
	return p.recovery, nil
}

func (p *currentChainProvider) Observe(context.Context) (sshservice.ObservationV1, error) {
	p.calls++
	if p.observe != nil {
		p.observe()
	}
	return sshservice.ObservationV1{Posture: p.posture, ProviderRevision: sshmanagement.Revision("provider")}, nil
}

func TestCurrentSSHOwnerCrossesSecondBoundaryThroughHostSurfaceAndManagement(t *testing.T) {
	for _, kind := range []string{"current", "future", "expired"} {
		t.Run(kind, func(t *testing.T) {
			clock := time.Unix(1000, 0).UTC()
			provider := &currentChainProvider{}
			provider.observe = func() {
				clock = time.Unix(1001, 0).UTC()
				observed := clock
				if kind == "future" {
					observed = observed.Add(time.Second)
				}
				if kind == "expired" {
					observed = observed.Add(-time.Hour)
				}
				provider.posture = stockDropbearPostureFixture(observed)
			}
			manager := sshservice.NewManager(sshservice.Repository{}, provider)
			manager.Now = func() time.Time { return clock }
			surfaceProvider := protectionhostsurface.NewProviderWithSSHProjection(protectionhostsurface.SSHPostureProjectionSource{Reader: manager})
			surfaceProvider.Now = manager.Now
			surfaceProvider.Resources = func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }
			surfaceProvider.ObservePlatform = func(context.Context, hostfacts.Limits) (protectionhostsurface.PlatformSnapshot, error) {
				return protectionhostsurface.PlatformSnapshot{Sockets: []protectionhostsurface.RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Protocol: "tcp"}}}, nil
			}
			observed, err := surfaceProvider.Observe(context.Background(), hostfacts.DefaultLimits())
			if err != nil || len(observed.Facts) != 1 || provider.calls != 1 {
				t.Fatalf("observation failed or split: %v calls=%d", err, provider.calls)
			}
			surface := observed.Facts[0]
			if kind != "current" {
				if surface.SemanticOwner != nil {
					t.Fatal("untrusted authority accepted")
				}
				return
			}
			if surface.SemanticOwner == nil || surface.Classification != hostfacts.ClassificationExpectedExternal || surface.LastSeen != 1001 {
				t.Fatalf("current owner discarded: %#v", surface)
			}
			resources, err := sshmanagement.ProtectableResources(provider.posture, clock)
			if err != nil {
				t.Fatal(err)
			}
			endpoints := hostmanagement.Endpoints(resources, hostfacts.Snapshot{Facts: observed.Facts}, clock)
			if len(endpoints) != 1 || endpoints[0].ID != provider.posture.Endpoints[0].ID || endpoints[0].Owner != sshmanagement.ProtectionResourceOwner || endpoints[0].RuntimeRevision != provider.posture.ListenerAuthorities[0].Revision {
				t.Fatalf("owner endpoint lost or duplicated: %#v", endpoints)
			}
			assertSSHPostureReachesPreparedTypedFirewallCandidate(t, clock, provider.posture)
		})
	}
}

func TestCurrentSSHProductionBaselineUsesOneOwnerGeneration(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, false, "")
}

func TestManagementPlanProductionHealthLifecycle(t *testing.T) {
	for _, fault := range []string{"", "panel", "subscription", "ssh_ipv4", "ssh_ipv6", "missing", "duplicate", "conflicting", "stale", "expired_endpoint", "future", "untrusted", "resource_absent", "family", "topology", "owner", "listener_unavailable", "rollback_failure"} {
		t.Run("health_"+fault, func(t *testing.T) { assertCurrentSSHProductionLifecycle(t, true, fault) })
	}
}

type managementRuntimeChecker struct{}

func (managementRuntimeChecker) Check(_ context.Context, id string) componenthealth.Result {
	return componenthealth.Result{ResourceID: id, Status: componenthealth.StatusOK, Check: "listener", FactCode: "listener_ready"}
}

func assertCurrentSSHProductionLifecycle(t *testing.T, exactHealth bool, fault string, bootRecovery ...string) {
	assertCurrentSSHProductionLifecycleWithOperator(t, exactHealth, fault, nil, bootRecovery...)
}

type lifecycleOperator func(Workflow, *protectionrepository.Repository, string) (Result, error)

// RunRetainedPublicLifecycleTest shares the durable production fixture with
// the external-package public route integration test. It is test-only.
func RunRetainedPublicLifecycleTest(t *testing.T, operator func(Workflow, *protectionrepository.Repository, string) (Result, error)) {
	assertCurrentSSHProductionLifecycleWithOperator(t, true, "", operator, "sqlite_expired_reconcile")
}

func assertCurrentSSHProductionLifecycleWithOperator(t *testing.T, exactHealth bool, fault string, operator lifecycleOperator, bootRecovery ...string) {
	// Native registries run on the real clock; the owner and baseline clocks
	// advance deterministically within the current bounded validity interval.
	clock := time.Now().UTC().Add(-10 * time.Second)
	provider := &currentChainProvider{}
	provider.observe = func() {
		clock = clock.Add(time.Second)
		provider.posture = stockDropbearPostureFixture(clock)
		v6Endpoint := provider.posture.Endpoints[0]
		v6Endpoint.ID, v6Endpoint.Family, v6Endpoint.Bind = "management:ssh:configured:ipv6:22:main:abcdef0123456789", hostresources.AddressFamilyIPv6, "::"
		v6Authority := provider.posture.ListenerAuthorities[0]
		v6Authority.EndpointIDs = []string{v6Endpoint.ID}
		v6Authority.Socket.Family, v6Authority.Socket.Bind, v6Authority.Socket.Inode = hostfacts.FamilyIPv6, "::", "23"
		v6Authority.Socket.Cookie = 43
		only := true
		v6Authority.Socket.IPv6Only = &only
		v6Authority.Socket.CoverageFamilies = []hostfacts.Family{hostfacts.FamilyIPv6}
		v6Authority.Seal()
		provider.posture.Endpoints = append(provider.posture.Endpoints, v6Endpoint)
		provider.posture.ListenerAuthorities = append(provider.posture.ListenerAuthorities, v6Authority)
		// Give each observation a different process generation so independently
		// refreshed owner paths cannot accidentally pass this composition test.
		for i := range provider.posture.ListenerAuthorities {
			provider.posture.ListenerAuthorities[i].Process.StartTime = strconv.Itoa(100 + provider.calls)
			provider.posture.ListenerAuthorities[i].Seal()
		}
		provider.posture.SemanticRevision = sshmanagement.PostureSemanticRevision(provider.posture)
	}
	manager := sshservice.NewManager(sshservice.Repository{}, provider)
	manager.Now = func() time.Time { return clock }
	previousResources, previousSurfaces, previousEvidence := hostresources.Default, hostfacts.Default, hostmanagement.DefaultEvidence
	hostresources.Default, hostfacts.Default, hostmanagement.DefaultEvidence = hostresources.NewRegistry(time.Second), hostfacts.NewRegistry(), hostmanagement.NewEvidenceRegistry()
	t.Cleanup(func() {
		hostresources.Default, hostfacts.Default, hostmanagement.DefaultEvidence = previousResources, previousSurfaces, previousEvidence
	})
	if _, err := hostresources.Register(sshservice.ProtectionResourceContributor{Reader: manager, Now: manager.Now}); err != nil {
		t.Fatal(err)
	}
	surfaceProvider := protectionhostsurface.NewProviderWithSSHProjection(protectionhostsurface.SSHPostureProjectionSource{Reader: manager})
	surfaceProvider.Now = manager.Now
	// The separate injected source supplies panel/subscription observations.
	surfaceProvider.Resources = func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }
	surfaceProvider.ObservePlatform = func(context.Context, hostfacts.Limits) (protectionhostsurface.PlatformSnapshot, error) {
		return protectionhostsurface.PlatformSnapshot{Sockets: []protectionhostsurface.RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Protocol: "tcp"}, {Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "::", Port: 22, Inode: "23", Protocol: "tcp"}}}, nil
	}
	if _, err := hostfacts.Register(surfaceProvider); err != nil {
		t.Fatal(err)
	}
	var startupDB *gorm.DB
	dbConfig := workflowDatabaseConfig{Configure: func(db *gorm.DB) { startupDB = db }}
	if len(bootRecovery) != 0 && strings.HasPrefix(bootRecovery[0], "sqlite_") {
		dbConfig.Open = productionStartupSQLite(t)
	}
	workflow, helper, _, repo, _ := newWorkflowWithRoot(t, nil, dbConfig)
	if operator != nil {
		shared := sshservice.Shared()
		previousProvider, previousNow := shared.Provider, shared.Now
		shared.Provider, shared.Now = provider, manager.Now
		t.Cleanup(func() { shared.Provider, shared.Now = previousProvider, previousNow })
	}
	workflow.Now = manager.Now
	baseline := NewBaselineService(repo)
	baseline.SSH, baseline.Now = manager, manager.Now
	boot := &controlledBootGeneration{generation: hostresources.Revision("boot-A")}
	if len(bootRecovery) != 0 {
		workflow.Runtime, workflow.RuntimeStore, workflow.CurrentRuntimeManagement = boot, repo, baseline.RuntimeManagement
	}
	state, err := baseline.Snapshot(context.Background(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("baseline refreshed SSH %d times", provider.calls)
	}
	if len(state.Management) != 2 || state.Management[0].ID != provider.posture.Endpoints[0].ID || state.Management[0].OwnerRevision != provider.posture.SemanticRevision {
		t.Fatalf("baseline lost current owner endpoint: %#v", state.Management)
	}
	for _, reason := range state.Plan.BaselineEligibility.ReasonCodes {
		if reason == "ssh_management_endpoint_missing" || reason == "management_endpoint_inventory_incomplete" {
			t.Fatalf("baseline lost SSH management: %s", reason)
		}
	}
	if len(state.Graph.Nodes) != 2 || len(state.Graph.Nodes[0].ObservedClaims) != 1 || slices.Contains(state.Graph.Nodes[0].ReasonCodes, "listener_deployment_mismatch") {
		t.Fatalf("baseline mixed resource/surface generation: %#v", state.Graph)
	}
	if state.Plan.BaselineEligibility.MutationReady {
		t.Fatal("missing operator prerequisites authorized mutation")
	}
	preview := Preview(state.Plan, PreviewOptions{IncludeGeneratedNFT: true})
	if strings.Contains(preview.GeneratedNFT, "flush ruleset") || strings.Contains(preview.GeneratedNFT, "table inet fw4") {
		t.Fatal("preview crossed owned namespace")
	}
	if collisions := protectionresources.DetectCollisions(state.Plan.Resources); len(collisions) != 0 {
		t.Fatalf("separate exact socket coverage became ambiguous: %#v", collisions)
	}
	if _, err := workflow.Prepare(context.Background(), PrepareInput{Plan: state.Plan, Actor: "ci", IdempotencyKey: "missing-prerequisites", Confirmation: "PREPARE SERVER PROTECTION " + state.Plan.Revision}); err == nil {
		t.Fatal("Prepare ignored missing operator prerequisites")
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
		t.Fatal("read-only boundary mutated firewall")
	}

	// Continue the same real baseline composition with explicit operator inputs.
	trusted := []string{"198.51.100.0/24", "2001:db8:1::/64"}
	if len(bootRecovery) != 0 {
		trusted = []string{"198.51.100.9/32", "2001:db8:1::9/128"}
	}
	for _, prefix := range trusted {
		if err := repo.CreateIPAllowlist(context.Background(), &protectionrepository.IPAllowlistModel{IPCIDR: prefix, Reason: "explicit fixture trust", CreatedBy: "ci"}); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := state.Management[0]
	provider.recovery = []hostresources.RecoveryPathV1{{
		Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:ssh-current", OperationBound: true, SingleUse: true, TargetOperation: "firewall-preflight", Revision: 1, Kind: string(endpoint.ServiceKind), EndpointID: endpoint.ID,
		PrincipalID: "principal:fixture", SourcePrefix: "198.51.100.0/24", VerificationMethod: "fresh_ssh_login",
		VerifiedAt: clock.Add(-time.Minute).Unix(), ExpiresAt: clock.Add(10 * time.Minute).Unix(), IndependenceClass: "independent_reconnect",
		VerificationState: "verified", SourceRevision: sshmanagement.Revision("ssh-login"), ConfigurationRevision: endpoint.ConfigurationRevision,
	}}
	if _, err := hostmanagement.RegisterEvidenceProvider(provider); err != nil {
		t.Fatal(err)
	}
	ready, err := baseline.Snapshot(context.Background(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || len(ready.Management) != 2 || !ready.Plan.BaselineEligibility.MutationReady {
		t.Fatalf("current dual-family baseline not ready: calls=%d eligibility=%#v", provider.calls, ready.Plan.BaselineEligibility)
	}
	if err := Preflight(ready.Plan); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ready.Plan.AllowTCPPorts, 22) {
		t.Fatal("SSH keep missing")
	}
	managementSource := renewalManagementSource{now: manager.Now, singleFamily: exactHealth}
	if _, err := hostresources.Register(managementSource); err != nil {
		t.Fatal(err)
	}
	if _, err := hostfacts.Register(managementSource); err != nil {
		t.Fatal(err)
	}
	ready, err = baseline.Snapshot(context.Background(), true, nil)
	expectedEndpoints := 6
	if exactHealth {
		expectedEndpoints = 4
	}
	if err != nil || len(ready.Management) != expectedEndpoints || len(ready.Plan.Endpoints) != expectedEndpoints || !ready.Plan.BaselineEligibility.MutationReady {
		t.Fatalf("R13 six-endpoint baseline: management=%d endpoints=%d err=%v", len(ready.Management), len(ready.Plan.Endpoints), err)
	}

	// Renew only observation lifetimes: the public preview binding must survive.
	provider.observe = func() {
		clock = clock.Add(time.Second)
		provider.posture.ObservedAt = clock.Unix()
		provider.posture.ExpiresAt = clock.Add(sshmanagement.MaxPostureLifetime).Unix()
		for i := range provider.posture.Endpoints {
			provider.posture.Endpoints[i].ObservedAt = clock.Unix()
			provider.posture.Endpoints[i].ExpiresAt = provider.posture.ExpiresAt
		}
		for i := range provider.posture.ListenerAuthorities {
			provider.posture.ListenerAuthorities[i].ObservedAt = clock.Unix()
			provider.posture.ListenerAuthorities[i].ExpiresAt = clock.Add(30 * time.Second).Unix()
			provider.posture.ListenerAuthorities[i].Seal()
		}
	}
	renewed, err := baseline.Snapshot(context.Background(), true, nil)
	if err != nil || renewed.Binding.Revision != ready.Binding.Revision || renewed.Plan.Revision != ready.Plan.Revision {
		t.Fatalf("clock-only baseline refresh changed preview binding: %v", err)
	}
	ready = renewed
	preview = Preview(ready.Plan, PreviewOptions{IncludeGeneratedNFT: true, NFTCapability: NFTPreviewCapability{Available: true, Revision: hostresources.Revision("fixture-capability")}})
	if preview.Revision != ready.Plan.Revision || preview.GeneratedNFT != RenderNFTPreview(ready.Plan) {
		t.Fatal("fresh R13 preview did not bind the candidate")
	}
	if exactHealth {
		if len(ready.Plan.Resources) != 4 {
			t.Fatal("physical resource shape differs")
		}
		workflow.Health = func(ctx context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
			owner := sshservice.ProtectionHealth{Reader: manager, Now: manager.Now}
			postMutation := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) > 0
			if postMutation && fault != "" {
				payload, _ := json.Marshal(provider.posture)
				var changed sshmanagement.SSHPostureV1
				_ = json.Unmarshal(payload, &changed)
				switch fault {
				case "stale":
					changed.ObservedAt, changed.ExpiresAt = clock.Add(-time.Hour).Unix(), clock.Add(-time.Minute).Unix()
				case "expired_endpoint":
					changed.Endpoints[0].ExpiresAt = clock.Unix()
				case "future":
					changed.ObservedAt = clock.Add(time.Minute).Unix()
				case "untrusted":
					changed.ListenerAuthorities[0].Revision = "untrusted"
				case "listener_unavailable":
					changed.ListenerAuthorities = nil
				case "resource_absent":
					changed.Endpoints = changed.Endpoints[:1]
					changed.ListenerAuthorities = changed.ListenerAuthorities[:1]
					changed.SemanticRevision = sshmanagement.PostureSemanticRevision(changed)
				case "family":
					changed.ListenerAuthorities[0].Socket.Family = hostfacts.FamilyIPv6
				case "topology":
					changed.ListenerAuthorities[0].Socket.Port++
				}
				owner.Reader = chainSSHPostureReader{posture: changed}
			}
			results := protectionhealth.EvaluateWithSSH(ctx, resources, managementRuntimeChecker{}, owner)
			if !postMutation || fault == "" {
				return results
			}
			for i, resource := range resources {
				_ = i
				for j := range results {
					if results[j].ResourceID != resource.ID {
						continue
					}
					if fault == "panel" && resource.Kind == "panel_web" || fault == "subscription" && resource.Kind == "subscription" ||
						fault == "ssh_ipv4" && resource.Kind == sshmanagement.ProtectionResourceKind && resource.Listen == "0.0.0.0" ||
						fault == "ssh_ipv6" && resource.Kind == sshmanagement.ProtectionResourceKind && resource.Listen == "::" || fault == "rollback_failure" {
						results[j].Status = componenthealth.StatusDegraded
					}
					if resource.Kind == sshmanagement.ProtectionResourceKind {
						switch fault {
						case "missing":
							return append(results[:j], results[j+1:]...)
						case "duplicate", "conflicting":
							duplicate := results[j]
							if fault == "conflicting" {
								duplicate.Status = componenthealth.StatusDegraded
							}
							return append(results, duplicate)
						case "owner":
							resource.Owner = "other-owner"
							results[j] = owner.Check(ctx, []hostresources.ProtectableResource{resource})[0]
						}
					}
				}
			}
			return results
		}
		workflow.RollbackHealth = func(ctx context.Context, _ []hostresources.ProtectableResource) []componenthealth.Result {
			return protectionhealth.EvaluateWithSSH(ctx, ready.Plan.Resources, managementRuntimeChecker{}, sshservice.ProtectionHealth{Reader: manager, Now: manager.Now})
		}
		if fault == "rollback_failure" {
			helper.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false}
		}
	}
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: ready.Plan, Actor: "ci", IdempotencyKey: "current-dual-family", Confirmation: "PREPARE SERVER PROTECTION " + ready.Plan.Revision})
	if err != nil || prepared.Operation.OperationID == "" {
		t.Fatalf("current dual-family Prepare failed: %v", err)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
		t.Fatal("Prepare mutated firewall")
	}
	// R13-MUT-001: refresh AFTER Prepare, through the real SSH, resource,
	// management and baseline owners, then exercise the persisted workflow.
	current, err := baseline.Snapshot(context.Background(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Plan.Revision != ready.Plan.Revision || current.Plan.InputRevision != ready.Plan.InputRevision || RenderManagedNFT(current.Plan) != RenderManagedNFT(ready.Plan) {
		t.Fatal("same semantics changed public plan/input/candidate")
	}
	if current.Plan.GraphRevision == ready.Plan.GraphRevision || current.Plan.OwnerObservationRevision == ready.Plan.OwnerObservationRevision {
		t.Fatal("fixture did not renew graph and owner observation generations")
	}
	beforeContribution, err := contributionFromPlan(ready.Plan)
	if err != nil {
		t.Fatal(err)
	}
	afterContribution, err := contributionFromPlan(current.Plan)
	if err != nil || beforeContribution.SemanticRevision != afterContribution.SemanticRevision {
		t.Fatalf("renewal changed contribution identity: %v", err)
	}
	// Prove the old whole-object algorithm rejects this very production shape.
	beforeContribution.SemanticRevision, afterContribution.SemanticRevision = "", ""
	if hostresources.Revision(beforeContribution) == hostresources.Revision(afterContribution) {
		t.Fatal("fixture does not reproduce the old R13 whole-object divergence")
	}
	operationID := prepared.Operation.OperationID
	storage := workflow.State.(*protectionartifacts.Storage)
	if storage.HasMutationMarker(operationID) {
		t.Fatal("Prepare created a mutation marker")
	}
	applyInput := ApplyInput{OperationID: operationID, Plan: current.Plan, Confirmation: "APPLY SERVER PROTECTION " + operationID}
	applied, err := workflow.Apply(context.Background(), applyInput)
	if fault != "" {
		if !errors.Is(err, ErrHealthFailed) || !applied.RollbackAttempted || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 1 {
			t.Fatalf("health failure lifecycle: state=%s err=%v", applied.State, err)
		}
		if fault == "rollback_failure" {
			if !errors.Is(err, ErrRollbackFailed) {
				t.Fatal("real rollback failure lost classification")
			}
		} else if applied.State != protectionoperations.StateRolledBack {
			t.Fatalf("health failure did not roll back: %s %v", applied.State, err)
		}
		if fault == "resource_absent" {
			for _, resource := range current.Plan.Resources {
				if resource.Kind != sshmanagement.ProtectionResourceKind {
					continue
				}
				for _, result := range applied.Health {
					if result.ResourceID == resource.ID && (result.Status == componenthealth.StatusOK) != (resource.Listen == "0.0.0.0") {
						t.Fatal("family loss affected the wrong SSH resource")
					}
				}
			}
		}
		return
	}
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("R13 renewed Apply: state=%s err=%v", applied.State, err)
	}
	checkpoint, err := workflow.loadCheckpoint(operationID)
	if err != nil || checkpoint.ContributionRevision != ContributionRevision(current.Plan) || checkpoint.PlanRevision != applied.PlanRevision {
		t.Fatalf("renewed checkpoint: %v", err)
	}
	if !storage.HasMutationMarker(operationID) || checkpoint.GraphRevision != current.Plan.GraphRevision {
		t.Fatal("Apply did not retain its current generation and mutation evidence")
	}
	artifact, err := repo.ArtifactByOperation(context.Background(), operationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	transition, err := repo.FirewallTransition(context.Background(), operationID)
	if err != nil || transition.State != "HEALTH_VERIFIED" || transition.MutationCompletedUnixNano <= transition.MarkerUnixNano || transition.MarkerUnixNano <= 0 {
		t.Fatalf("applied transition: %#v err=%v", transition, err)
	}
	authority, err := repo.FirewallAuthority(context.Background())
	if err != nil || !authority.HasComposition || len(authority.Contributions) != 1 || authority.Contributions[0].SemanticRevision != checkpoint.ContributionRevision {
		t.Fatalf("committed contribution: %v", err)
	}
	// Serialization remains observation-rich while replay consumes semantic identity.
	var stored ManagedFirewallContributionV1
	if json.Unmarshal(authority.Contributions[0].SemanticJSON, &stored) != nil || stored.Baseline == nil || stored.Baseline.GraphRevision == "" {
		t.Fatal("committed contribution lost observation evidence")
	}
	if len(bootRecovery) == 0 {
		replay, err := workflow.Apply(context.Background(), applyInput)
		if err != nil || replay.State != protectionoperations.StateApplied || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 {
			t.Fatalf("renewed applied replay: %v", err)
		}
	}
	if len(bootRecovery) != 0 {
		if strings.HasPrefix(bootRecovery[0], "sqlite_package_") {
			assertPackageReplacement(t, &workflow, helper, startupDB, storage, baseline, operationID, strings.TrimPrefix(bootRecovery[0], "sqlite_package_"), func(next string) { fault = next })
			return
		}
		before := authority.Composition
		if before.Runtime.VerifiedBoot != boot.generation {
			t.Fatal("Apply did not seal its boot authority")
		}
		if err := workflow.Manager.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		rootPath := storage.Root()
		if err := os.RemoveAll(rootPath); err != nil {
			t.Fatal(err)
		}
		helper.ManagedTablePresent, helper.ManagedPlanRevision, helper.ManagedCandidateSHA, helper.ManagedCandidateSemantic, helper.ManagedTimedMembership = false, "", "", "", ""
		provider.recovery = nil
		// The native registry validates against the real clock. Restoration
		// performs more refreshes than Apply; never advance fixture time into
		// the future just because the lifecycle completed in under a second.
		refresh := provider.observe
		provider.observe = func() { clock = time.Now().UTC().Add(-2 * time.Second); refresh() }
		boot.generation = hostresources.Revision("boot-B")
		scenario := bootRecovery[0]
		switch scenario {
		case "same_boot":
			boot.generation = before.Runtime.VerifiedBoot
		case "waiting":
			workflow.CurrentRuntimeManagement = func(context.Context) (RuntimeManagement, error) { return RuntimeManagement{}, ErrUnsafeResource }
		case "stale_management":
			workflow.CurrentRuntimeManagement = func(ctx context.Context) (RuntimeManagement, error) {
				current, err := baseline.RuntimeManagement(ctx)
				current.ObservedAt = current.ObservedAt.Add(-time.Hour)
				return current, err
			}
		case "integrity":
			workflow.Contributions = mismatchedRuntimeStore{repo}
		case "interrupted_before_apply", "interrupted_after_apply":
			workflow.RuntimeStore = &interruptedRuntimeStore{RuntimeStore: repo, beforeApply: scenario == "interrupted_before_apply"}
		case "validation_failure":
			helper.Responses[protectionhelper.OperationNFTValidate] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeValidationFailed}
		case "apply_failure":
			helper.FailAfter[protectionhelper.OperationNFTApply] = errors.New("lost post-mutation response")
		case "health_failure", "sqlite_cleanup_failed":
			if scenario == "sqlite_cleanup_failed" {
				helper.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeValidationFailed}
			}
			priorHealth := workflow.Health
			workflow.Health = func(ctx context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
				result := priorHealth(ctx, resources)
				if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) > 1 {
					result[0].Status = componenthealth.StatusDegraded
				}
				return result
			}
		case "foreign":
			helper.ManagedTablePresent, helper.ManagedPlanRevision, helper.ManagedCandidateSemantic, helper.ManagedTimedMembership = true, hostresources.Revision("foreign"), hostresources.Revision("foreign"), before.CandidateTimedMembershipSHA256
		}
		storage, err = protectionartifacts.NewWithRecoveryProjection(rootPath, sptest.SystemdRecoveryProjection())
		if err != nil {
			t.Fatal(err)
		}
		restarted := protectionoperations.NewManager(repo, protectionoperations.Options{InstanceID: "committed-boot-B", PID: 78, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
		if scenario == "sqlite_stuck_r16" || scenario == "sqlite_expired_reconcile" {
			original, err := repo.OperationByID(t.Context(), operationID)
			if err != nil {
				t.Fatal(err)
			}
			pending := before.Runtime
			pending.State, pending.Reason, pending.AttemptBoot = "RESTORE_REQUIRED", "boot_runtime_loss_detected", boot.generation
			pending.StartedAt, pending.Deadline = time.Now().Add(-10*time.Minute).Unix(), time.Now().Add(-5*time.Minute).Unix()
			pending.HealthAt, pending.HealthRevision = 0, ""
			if err := repo.UpdateFirewallRuntime(t.Context(), operationID, original.Revision, before.Revision, before.Runtime, pending); err != nil {
				t.Fatal(err)
			}
			// Reconstruct only the persisted stuck state, without touching any
			// physical DB or calling Apply. The old process is proven gone.
			stuckState := protectionoperations.StateRestoringRuntime
			if scenario == "sqlite_expired_reconcile" {
				stuckState = protectionoperations.StateReconcileRequired
			}
			if err := startupDB.Model(&protectionrepository.OperationLockModel{}).Where("operation_id = ?", operationID).Updates(map[string]any{"state": stuckState, "expires_at": time.Now().Add(-time.Hour).Unix()}).Error; err != nil {
				t.Fatal(err)
			}
			restarted = protectionoperations.NewManager(repo, protectionoperations.Options{InstanceID: "stuck-r16-upgrade", PID: 78, PIDProbe: stoppedRuntimePID{}, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
		}
		t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
		client, clientErr := protectionhelper.NewClient(sptest.ManagedRoot(t, rootPath), restarted, helper, &helperAudit{})
		if clientErr != nil {
			t.Fatal(clientErr)
		}
		workflow.Manager, workflow.Helper, workflow.Marker, workflow.State = restarted, client, storage, storage
		workflow.Artifacts = protectionartifacts.Service{Storage: storage, Store: repo}
		if len(bootRecovery) == 3 && bootRecovery[1] == "process_seed" {
			writeExpiredProcessSeed(t, startupDB, rootPath, operationID, before.Revision, bootRecovery[2])
			return
		}
		restartExpiredOwner := func() {
			t.Helper()
			if err := restarted.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			workflow.RuntimeStore, workflow.Contributions = repo, repo
			restarted = protectionoperations.NewManager(repo, protectionoperations.Options{InstanceID: "expired-owner-restart", PID: 79, PIDProbe: stoppedRuntimePID{}, Now: func() time.Time { return time.Now().Add(time.Hour) }, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
			t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
			client, err = protectionhelper.NewClient(sptest.ManagedRoot(t, rootPath), restarted, helper, &helperAudit{})
			if err != nil {
				t.Fatal(err)
			}
			workflow.Manager, workflow.Helper = restarted, client
			if err := restarted.SetReconcilerForKind(protectionoperations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
				t.Fatal(err)
			}
			if err := restarted.SetRecovery(BackendRecovery{Helper: client, Manager: restarted, Storage: storage, Repository: repo, Health: workflow.RollbackHealth, Workflow: &workflow}); err != nil {
				t.Fatal(err)
			}
			if err := restarted.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if err := restarted.SetReconcilerForKind(protectionoperations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
			t.Fatal(err)
		}
		finishAudit := func() {}
		if scenario == "sqlite_concurrent_audit" {
			finishAudit = runtimeConcurrentAuditBarrier(t, startupDB, operationID)
		}

		finishFailure := func() {}
		if scenario == "sqlite_readonly_fence" {
			finishFailure = runtimeReadonlyFence(t, startupDB)
		}
		var startErr error
		if len(bootRecovery) > 1 && bootRecovery[1] == "generation_unavailable" {
			workflow.Runtime = unavailableRuntimeGeneration{}
		}
		if len(bootRecovery) > 1 && (bootRecovery[1] == "before_terminal" || bootRecovery[1] == "after_terminal") {
			workflow.RuntimeStore = &crashExpiredRuntime{RuntimeStore: repo, after: bootRecovery[1] == "after_terminal"}
			_ = runUntilRuntimeCrash(t, func() error { return restarted.Start(t.Context()) })
			restartExpiredOwner()
		} else if strings.HasPrefix(scenario, "sqlite_crash_") {
			window := strings.TrimPrefix(scenario, "sqlite_crash_")
			if window == "before_checkpoint" || window == "after_checkpoint" {
				workflow.State = &crashRuntimeCheckpoint{StateStore: storage, after: window == "after_checkpoint"}
			} else {
				workflow.RuntimeStore = &crashRuntimeStore{RuntimeStore: repo, window: window}
			}
			startErr = runUntilRuntimeCrash(t, func() error { return restarted.Start(t.Context()) })
		} else {
			startErr = restarted.Start(t.Context())
		}
		finishAudit()
		if scenario == "sqlite_readonly_fence" {
			finishFailure()
			pending, err := repo.FirewallAuthority(t.Context())
			original, opErr := repo.OperationByID(t.Context(), operationID)
			if startErr != nil || err != nil || opErr != nil || !pending.HasComposition || pending.Composition.Runtime.State != "RESTORE_REQUIRED" || pending.Composition.Runtime.MutationAt != 0 || pending.Composition.Runtime.HealthAt != 0 || original.RecoveryAttempts != 1 || original.RecoveryErrorCode != "runtime_restore_execution_failed" || helper.ManagedTablePresent || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 {
				t.Fatal("persistence failure crossed mutation fence or destroyed runtime owner")
			}
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasPrefix(scenario, "interrupted_") || strings.HasPrefix(scenario, "sqlite_crash_") {
			if strings.HasPrefix(scenario, "interrupted_") {
				interruption := workflow.RuntimeStore.(*interruptedRuntimeStore)
				failed, loadErr := repo.OperationByID(t.Context(), operationID)
				if startErr != nil || !interruption.fired || loadErr != nil || failed.RecoveryAttempts != 1 || failed.RecoveryErrorCode != "runtime_restore_execution_failed" {
					t.Fatalf("interruption did not retain its running recovery owner and durable failure: %v/%v attempts=%d", startErr, loadErr, failed.RecoveryAttempts)
				}

			}
			if err := restarted.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			workflow.RuntimeStore, workflow.State = repo, storage
			restarted = protectionoperations.NewManager(repo, protectionoperations.Options{InstanceID: "committed-boot-B-restart", PID: 79, PIDProbe: stoppedRuntimePID{}, Now: func() time.Time { return time.Now().Add(time.Hour) }, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
			t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
			client, err = protectionhelper.NewClient(sptest.ManagedRoot(t, rootPath), restarted, helper, &helperAudit{})
			if err != nil {
				t.Fatal(err)
			}
			workflow.Manager, workflow.Helper = restarted, client
			if err := restarted.SetReconcilerForKind(protectionoperations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
				t.Fatal(err)
			}
			if err := restarted.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
		} else if startErr != nil {
			t.Fatal(startErr)
		}
		if scenario == "sqlite_cleanup_failed" {
			// The failed helper response leaves the first execution fence live.
			// One recovery pass seals its already durable failure; later passes
			// must be no-ops until an external fact changes.
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			previousRevision := 0
			for attempt := 0; attempt < 4; attempt++ {
				retained, err := repo.FirewallAuthority(t.Context())
				original, opErr := repo.OperationByID(t.Context(), operationID)
				if err != nil || opErr != nil || !retained.HasComposition || retained.Composition.Runtime.State != "RESTORE_FAILED" || !retained.Composition.Runtime.CleanupAttempted || original.State != protectionoperations.StateReconcileRequired || !helper.ManagedTablePresent {
					t.Fatal("failed cleanup fixture lost retained authority")
				}
				if attempt > 0 && previousRevision != original.Revision {
					t.Fatal("failed cleanup advanced revision without a semantic change")
				}
				previousRevision = original.Revision
				if _, err := restarted.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 2 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 1 {
				t.Fatal("failed cleanup retried a mutation")
			}
			// A new typed observation is a real fact: once the failed cleanup's
			// table is absent, the existing retirement owner may finish once.
			helper.ManagedTablePresent, helper.ManagedPlanRevision, helper.ManagedCandidateSHA, helper.ManagedCandidateSemantic, helper.ManagedTimedMembership = false, "", "", "", ""
			workflow.Runtime = unavailableRuntimeGeneration{}
			blocked, err := repo.OperationByID(t.Context(), operationID)
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if _, err := restarted.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			unchanged, err := repo.OperationByID(t.Context(), operationID)
			if err != nil || unchanged.Revision != blocked.Revision || unchanged.State != blocked.State {
				t.Fatal("unavailable cleanup generation caused no-op revision churn")
			}
			workflow.Runtime = boot
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			retired, err := repo.FirewallAuthority(t.Context())
			original, opErr := repo.OperationByID(t.Context(), operationID)
			if err != nil || opErr != nil || retired.HasComposition || len(retired.Contributions) != 0 || original.State != protectionoperations.StateForgotten || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 2 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 1 {
				t.Fatal("changed absence did not permit the existing failure retirement")
			}
			return
		}
		if scenario == "sqlite_stuck_r16" || scenario == "sqlite_expired_reconcile" {
			stableRevision := 0
			var stableRuntime protectionrepository.FirewallRuntimeBinding
			for attempt := 0; attempt < 3; attempt++ {
				retained, err := repo.FirewallAuthority(t.Context())
				original, opErr := repo.OperationByID(t.Context(), operationID)
				t.Logf("expired retained tuple: interval=%d operation=%s revision=%d runtime=%s reason=%s", attempt, original.State, original.Revision, retained.Composition.Runtime.State, retained.Composition.Runtime.Reason)
				if scenario == "sqlite_expired_reconcile" && attempt > 0 && original.Revision != stableRevision {
					t.Errorf("no semantic progress but operation revision advanced: %d -> %d", stableRevision, original.Revision)
				}
				if attempt > 0 && retained.Composition.Runtime != stableRuntime {
					t.Fatal("periodic observation changed retained runtime authority")
				}
				stableRevision = original.Revision
				stableRuntime = retained.Composition.Runtime
				if err != nil || opErr != nil || !retained.HasComposition || len(retained.Contributions) != 1 || retained.Composition.Revision != before.Revision || retained.Composition.AppliedOperationID != operationID || retained.Composition.Runtime.State != "RESTORE_FAILED" || retained.Composition.Runtime.Reason != "boot_restore_deadline_expired" || retained.Composition.Runtime.MutationAt != 0 || retained.Composition.Runtime.HealthAt != 0 || original.State != protectionoperations.StateReconcileRequired || helper.ManagedTablePresent || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
					t.Fatalf("stuck R16 recovery lost truthful retained authority: runtime=%+v state=%s err=%v/%v", retained.Composition.Runtime, original.State, err, opErr)
				}
				if _, err := workflow.ReconcileAuthority(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := restarted.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			t.Log("stuck-R16: expired attempt terminalized without mutation, row deletion, operation replacement or false health; repeated reconciliation stays safe")
			if scenario == "sqlite_expired_reconcile" {
				original, err := repo.OperationByID(t.Context(), operationID)
				if err != nil || !workflow.CanResolveByRollback(t.Context(), original) {
					t.Fatal("retained authority has no normal operator rollback")
				}
				if len(bootRecovery) > 1 && bootRecovery[1] == "operator_negatives" {
					assertRetainedRollbackNegatives(t, workflow, repo, startupDB, helper, original)
				}
				if _, err := workflow.Rollback(t.Context(), operationID, ""); !errors.Is(err, protectionoperations.ErrConfirmationRequired) {
					t.Fatal("operator retirement bypassed confirmation")
				}
				var resolved Result
				if len(bootRecovery) > 1 && (bootRecovery[1] == "before_retirement" || bootRecovery[1] == "after_retirement") {
					workflow.Contributions = &crashRetirementCommit{FirewallContributionStore: repo, after: bootRecovery[1] == "after_retirement"}
					_ = runUntilRuntimeCrash(t, func() error {
						_, rollbackErr := workflow.Rollback(t.Context(), operationID, "ROLLBACK SERVER PROTECTION "+operationID)
						return rollbackErr
					})
					restartExpiredOwner()
					final, loadErr := repo.OperationByID(t.Context(), operationID)
					err = loadErr
					resolved = Result{OperationID: final.OperationID, State: final.State, Revision: final.Revision}
				} else if operator != nil {
					resolved, err = operator(workflow, repo, operationID)
				} else {
					resolved, err = workflow.Rollback(t.Context(), operationID, "ROLLBACK SERVER PROTECTION "+operationID)
				}
				if err != nil || resolved.State != protectionoperations.StateRolledBack {
					t.Fatalf("normal operator retirement failed: state=%s err=%v", resolved.State, err)
				}
				clean, err := repo.FirewallAuthority(t.Context())
				if err != nil || clean.HasComposition || len(clean.Contributions) != 0 || helper.ManagedTablePresent || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
					t.Fatal("operator retirement did not reach a mutation-free clean baseline")
				}
				workflow.Runtime = boot
				fresh, err := baseline.Snapshot(t.Context(), true, nil)
				if err != nil || len(fresh.Management) == 0 {
					t.Fatal("fresh operator management evidence unavailable")
				}
				endpoint := fresh.Management[0]
				provider.recovery = []hostresources.RecoveryPathV1{{
					Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:ssh-after-retirement", OperationBound: true, SingleUse: true, TargetOperation: "firewall-preflight", Revision: 1, Kind: string(endpoint.ServiceKind), EndpointID: endpoint.ID,
					PrincipalID: "principal:fixture", SourcePrefix: "198.51.100.0/24", VerificationMethod: "fresh_ssh_login",
					VerifiedAt: clock.Add(-time.Second).Unix(), ExpiresAt: clock.Add(10 * time.Minute).Unix(), IndependenceClass: "independent_reconnect",
					VerificationState: "verified", SourceRevision: sshmanagement.Revision("new-ssh-login-after-retirement"), ConfigurationRevision: endpoint.ConfigurationRevision,
				}}
				assertFreshLifecycleAfterRetirement(t, workflow, baseline, helper, repo, boot, operator)
			}
			return
		}
		if scenario == "integrity" || scenario == "stale_management" {
			pending, err := repo.FirewallAuthority(t.Context())
			if err != nil || !pending.HasComposition || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 {
				t.Fatal("unsafe restore consumed authority or mutated")
			}
			if scenario == "stale_management" {
				deadline := pending.Composition.Runtime.Deadline
				workflow.Now = func() time.Time { return time.Unix(deadline+1, 0) }
			}
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 {
				t.Fatal("unsafe restore retried mutation")
			}
			return
		}
		if scenario == "waiting" {
			pending, err := repo.FirewallAuthority(t.Context())
			if err != nil || !pending.HasComposition || pending.Composition.Runtime.State != "RESTORE_REQUIRED" || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 1 {
				t.Fatal("prerequisite wait lost authority or mutated runtime")
			}
			workflow.CurrentRuntimeManagement = baseline.RuntimeManagement
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if scenario == "same_boot" || scenario == "validation_failure" || scenario == "apply_failure" || scenario == "health_failure" || scenario == "foreign" || scenario == "interrupted_before_apply" || scenario == "sqlite_crash_after_fence" {
			state, err := repo.OperationByID(t.Context(), operationID)
			if err != nil {
				t.Fatal(err)
			}
			expectedApplies := 1
			if scenario == "apply_failure" || scenario == "health_failure" {
				expectedApplies = 2
			}
			if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != expectedApplies {
				t.Fatal("incorrect restore attempt count")
			}
			if scenario == "foreign" {
				if state.State != protectionoperations.StateReconcileRequired || !helper.ManagedTablePresent || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 0 {
					t.Fatal("foreign table was adopted or mutated")
				}
			} else if scenario == "interrupted_before_apply" || scenario == "sqlite_crash_after_fence" {
				retained, loadErr := repo.FirewallAuthority(t.Context())
				if state.State != protectionoperations.StateReconcileRequired || helper.ManagedTablePresent || loadErr != nil || !retained.HasComposition || retained.Composition.Revision != before.Revision || retained.Composition.Runtime.State != "RESTORE_FAILED" || retained.Composition.Runtime.Reason != "boot_restore_interrupted_absent" {
					t.Fatal("interrupted fence lost committed authority or became mutable")
				}
			} else if state.State != protectionoperations.StateForgotten || helper.ManagedTablePresent {
				t.Fatalf("unsafe restore did not reach safe retirement: %s", state.State)
			}
			if _, err := restarted.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != expectedApplies {
				t.Fatal("failed restore was retried")
			}
			return
		}
		after, err := repo.FirewallAuthority(t.Context())
		if err != nil || !after.HasComposition || after.Composition.Revision != before.Revision || after.Composition.CandidateSemanticSHA256 != before.CandidateSemanticSHA256 || after.Composition.AppliedOperationID != operationID || after.Composition.Runtime.VerifiedBoot != boot.generation || after.Composition.Runtime.State != "HEALTH_VERIFIED" || after.Composition.Runtime.HealthAt <= after.Composition.Runtime.MutationAt || after.Observation.State != FirewallLiveMatching {
			for _, request := range helper.Requests {
				if request.Operation != protectionhelper.OperationCapabilities {
					t.Logf("helper operation: %s", request.Operation)
				}
			}
			op, _ := repo.OperationByID(t.Context(), operationID)
			tr, _ := repo.FirewallTransition(t.Context(), operationID)
			t.Logf("operation=%s observer=%s retirement=%s", op.State, after.Observation.Reason, tr.RuntimeRetirementReason)
			t.Fatalf("boot continuation did not preserve exact healthy authority: %+v %v", after.Composition.Runtime, err)
		}
		if _, err := restarted.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 2 {
			t.Fatal("boot restore missing or duplicated")
		}
	}
	rolled, err := workflow.Rollback(context.Background(), operationID, "ROLLBACK SERVER PROTECTION "+operationID)
	if err != nil || rolled.State != protectionoperations.StateRolledBack {
		t.Fatalf("R13 normal rollback: state=%s err=%v", rolled.State, err)
	}
	authority, err = repo.FirewallAuthority(context.Background())
	if err != nil || authority.HasComposition || len(authority.Contributions) != 0 || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != 1 {
		t.Fatalf("rollback did not restore empty authority: %v", err)
	}
	terminal, err := repo.OperationByID(context.Background(), operationID)
	expectedRecoveryAttempts := 0
	if len(bootRecovery) != 0 && (strings.HasPrefix(bootRecovery[0], "interrupted_") || bootRecovery[0] == "sqlite_readonly_fence") {
		expectedRecoveryAttempts = 1
	}
	if err != nil || terminal.State != protectionoperations.StateRolledBack || terminal.RecoveryAttempts != expectedRecoveryAttempts || terminal.Revision <= applied.Revision {
		t.Fatalf("terminal operation: %#v err=%v", terminal, err)
	}
	protected, err := repo.ProtectedArtifactOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, live := protected[operationID]; live {
		t.Fatal("terminal rollback retained orphan live recovery authority")
	}
	if len(bootRecovery) != 0 {
		boot.generation = hostresources.Revision("boot-C")
		if _, err := workflow.Manager.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 2 || helper.ManagedTablePresent {
			t.Fatal("rolled-back operation was restored on another boot")
		}
	}
	// Marker/checkpoint are bounded historical evidence until ordinary artifact
	// retention removes their operation directory, not pending mutation authority.
	if !storage.HasMutationMarker(operationID) {
		t.Fatal("rollback lost retained mutation history")
	}
}
