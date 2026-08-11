package recoverypath

import (
	"context"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type recoveryStoreFake struct {
	upserts       []protectionrepository.RecoveryPathModel
	invalidations []string
	sources       []string
}

func (f *recoveryStoreFake) UpsertRecoveryPath(_ context.Context, value protectionrepository.RecoveryPathModel) error {
	f.upserts = append(f.upserts, value)
	return nil
}
func (f *recoveryStoreFake) InvalidateRecoveryPaths(_ context.Context, kind, principal, reason string) error {
	f.invalidations = append(f.invalidations, kind+"|"+principal+"|"+reason)
	return nil
}
func (f *recoveryStoreFake) InvalidateRecoveryPathsBySourceRevision(_ context.Context, kind, revision, reason string) error {
	f.sources = append(f.sources, kind+"|"+revision+"|"+reason)
	return nil
}

type recoveryHelperFake struct{ response protectionhelper.Response }

func (f recoveryHelperFake) Execute(context.Context, protectionhelper.Request) (protectionhelper.Response, error) {
	return f.response, nil
}

func recoveryEndpoint(kind hostresources.ManagementServiceKind, family hostresources.AddressFamily, id string, now time.Time) hostresources.ManagementEndpointV1 {
	return hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: id, Network: hostresources.NetworkTCP, Family: family, Bind: "0.0.0.0", Port: 22, ServiceKind: kind, Owner: "system", RecoveryPolicy: "fresh_independent_path_required", Source: "fixture", ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("c", 64)}
}

func TestPanelWriterMaterializesOnlyFreshIndependentLoginAndInvalidatesLifecycleEvents(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	store := &recoveryStoreFake{}
	endpoint := recoveryEndpoint(hostresources.ManagementPanel, hostresources.AddressFamilyIPv4, "management:panel", now)
	endpoint.Port, endpoint.Bind = 443, "192.0.2.5"
	writer := PanelWriter{Store: store, Now: func() time.Time { return now }, Endpoints: func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint}
	}}
	fields := map[string]string{"user": "admin", "ip": "198.51.100.10", "sessionRevision": strings.Repeat("a", 64)}
	if err := writer.Handle("login_success", fields); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("fresh panel login produced %d records", len(store.upserts))
	}
	row := store.upserts[0]
	if row.EndpointID != endpoint.ID || row.SourcePrefix != "198.51.100.10/32" || row.VerificationMethod != "fresh_panel_login" || row.IndependenceClass != "independent_reconnect" || row.ConfigurationRevision != endpoint.ConfigurationRevision || !validRevision(row.SourceRevision) || row.ExpiresAt-row.VerifiedAt != int64(RecoveryPathLifetime/time.Second) {
		t.Fatalf("panel recovery record is not exactly bound: %#v", row)
	}
	for _, rejected := range []map[string]string{{"user": "admin", "ip": "127.0.0.1", "sessionRevision": strings.Repeat("a", 64)}, {"user": "admin", "ip": "198.51.100.10", "sessionRevision": "bad"}} {
		if err := writer.Handle("login_success", rejected); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.upserts) != 1 {
		t.Fatal("dependent tunnel or malformed session became production RecoveryPath")
	}
	for _, event := range []string{"logout", "logout_all_admins", "admin_credentials_changed", "admin_deleted"} {
		if err := writer.Handle(event, map[string]string{"user": "admin"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.invalidations) != 4 {
		t.Fatalf("panel lifecycle invalidation count=%d", len(store.invalidations))
	}
}

func TestSSHObserverBindsFreshPublicKeyAuthenticationToOneExactEndpoint(t *testing.T) {
	now := time.Unix(20_000, 0).UTC()
	verifier := strings.Repeat("a", 64)
	store := &recoveryStoreFake{}
	endpoint := recoveryEndpoint(hostresources.ManagementSSH, hostresources.AddressFamilyIPv4, "management:ssh:primary", now)
	response := protectionhelper.Response{OK: true, SSHRecovery: &protectionhelper.SSHRecoveryResult{VerifierRevision: verifier, Observations: []protectionhelper.SSHRecoveryObservation{{ObservationID: "recovery:" + strings.Repeat("e", 64), PrincipalID: "principal:" + strings.Repeat("b", 64), SourcePrefix: "198.51.100.10/32", AuthenticationClass: "publickey", ObservedAt: now.Unix(), ObservedAtMicros: now.UnixMicro()}}}}
	observer := &SSHObserver{Store: store, Helper: recoveryHelperFake{response: response}, InstanceID: "instance", Now: func() time.Time { return now }, SinceMicros: now.Add(-time.Second).UnixMicro(), Endpoints: func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint}
	}}
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 1 || len(store.sources) != 1 {
		t.Fatalf("SSH observer did not seal one verifier-bound record: upserts=%d source-checks=%d", len(store.upserts), len(store.sources))
	}
	row := store.upserts[0]
	if row.EndpointID != endpoint.ID || row.SourcePrefix != "198.51.100.10/32" || row.VerificationMethod != "fresh_ssh_login" || row.SourceRevision != verifier || row.ConfigurationRevision != endpoint.ConfigurationRevision {
		t.Fatalf("SSH recovery record is not exact: %#v", row)
	}

	store.upserts = nil
	observer.SinceMicros = now.Add(-time.Second).UnixMicro()
	observer.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint, endpoint}
	}
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 0 {
		t.Fatal("ambiguous/wrong SSH endpoint became production RecoveryPath")
	}

	response.SSHRecovery.Observations[0].AuthenticationClass = "qga"
	observer.Helper = recoveryHelperFake{response: response}
	observer.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint}
	}
	observer.SinceMicros = now.Add(-time.Second).UnixMicro()
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 0 {
		t.Fatal("QGA was accepted as production RecoveryPath")
	}
}

func TestConfiguredManagementEndpointStaysExactlyScoped(t *testing.T) {
	now := time.Unix(6_000, 0).UTC()
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "core", Protocol: "tcp", Listen: "192.0.2.20", Port: 443,
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: strings.Repeat("b", 64), ConfigRevision: strings.Repeat("c", 64)}}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	endpoints := ManagementEndpoints([]hostresources.ProtectableResource{resource}, hostfacts.Snapshot{}, now)
	if len(endpoints) != 1 || endpoints[0].Family != hostresources.AddressFamilyIPv4 || endpoints[0].Network != hostresources.NetworkTCP || endpoints[0].Bind != resource.Listen || endpoints[0].Port != uint16(resource.Port) || endpoints[0].RecoveryPolicy != "fresh_independent_path_required" {
		t.Fatalf("management endpoint was widened: %#v", endpoints)
	}
}
