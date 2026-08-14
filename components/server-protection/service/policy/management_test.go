package policy

import (
	"slices"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestManagementGuardProtectsTrustedSourceAndLastRecoveryPath(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	endpoint := managementFixture(now)
	path := recoveryFixture(endpoint.ID, "192.0.2.0/24", now)
	base := ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: endpoint.ResourceID}, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, EndpointKey: hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: endpoint.Bind, Port: endpoint.Port}, Management: []hostresources.ManagementEndpointV1{endpoint}, RecoveryPaths: []hostresources.RecoveryPathV1{path}, MayRestrictTraffic: true, Now: now}
	lastPath := EvaluateManagementGuard(base)
	if lastPath.ActionAllowed || lastPath.State != ManagementGuardProtected || !slices.Contains(lastPath.ReasonCodes, "last_recovery_path_protected") {
		t.Fatalf("last recovery path was not protected: %#v", lastPath)
	}
	base.TrustedSources = []string{"192.0.2.0/24"}
	trusted := EvaluateManagementGuard(base)
	if trusted.ActionAllowed || !trusted.TrustedSourceMatched || !slices.Contains(trusted.ReasonCodes, "trusted_source_precedence") {
		t.Fatalf("trusted management source did not win precedence: %#v", trusted)
	}
}

func TestManagementGuardAllowsWhenIndependentPathRemains(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	endpoint := managementFixture(now)
	paths := []hostresources.RecoveryPathV1{recoveryFixture(endpoint.ID, "198.51.100.0/24", now), {Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:console", Kind: string(hostresources.ManagementPanel), EndpointID: endpoint.ID, PrincipalID: "principal:console", VerificationMethod: "provider_console", EvidenceProvider: "provider-console", TargetOperation: "firewall-preflight", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(), IndependenceClass: "provider_control_plane", VerificationState: "verified", OperationBound: true, Revision: 1, SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: endpoint.ConfigurationRevision}}
	result := EvaluateManagementGuard(ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: endpoint.ResourceID}, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, EndpointKey: hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: endpoint.Bind, Port: endpoint.Port}, Management: []hostresources.ManagementEndpointV1{endpoint}, RecoveryPaths: paths, MayRestrictTraffic: true, Now: now})
	if !result.ActionAllowed || result.State != ManagementGuardAllowed || len(result.UnaffectedRecoveryPathIDs) != 2 {
		t.Fatalf("independent recovery path was not preserved: %#v", result)
	}
}

func TestManagementGuardFailsClosedOnUnknownManagementFact(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	endpoint := managementFixture(now)
	endpoint.ConfigurationRevision = ""
	result := EvaluateManagementGuard(ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: endpoint.ResourceID}, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, Management: []hostresources.ManagementEndpointV1{endpoint}, RecoveryPaths: []hostresources.RecoveryPathV1{recoveryFixture(endpoint.ID, "198.51.100.0/24", now)}, MayRestrictTraffic: true, Now: now})
	if result.ActionAllowed || result.State != ManagementGuardUnknown || !slices.Contains(result.ReasonCodes, "management_endpoint_unknown") {
		t.Fatalf("unknown management endpoint did not fail closed: %#v", result)
	}
}

func TestManagementGuardRequiresRecoveryForEachExactEndpoint(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	primary := managementFixture(now)
	secondary := managementFixture(now)
	secondary.ID = "management:secondary"
	secondary.Port = 8443
	paths := []hostresources.RecoveryPathV1{
		recoveryFixture(primary.ID, "192.0.2.0/24", now),
		recoveryFixture(secondary.ID, "198.51.100.0/24", now),
	}
	paths[1].ID = "recovery:secondary"
	result := EvaluateManagementGuard(ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: primary.ResourceID}, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, EndpointKey: hostresources.PublicEndpointKey{Network: primary.Network, AddressFamily: primary.Family, BindAddress: primary.Bind, Port: primary.Port}, Management: []hostresources.ManagementEndpointV1{primary, secondary}, RecoveryPaths: paths, MayRestrictTraffic: true, Now: now})
	if result.ActionAllowed || result.State != ManagementGuardProtected || !slices.Contains(result.ReasonCodes, "last_recovery_path_protected") || slices.Contains(result.ProtectedEndpointIDs, secondary.ID) {
		t.Fatalf("another endpoint's recovery path bypassed the exact last-path guard: %#v", result)
	}
}

func TestManagementGuardRejectsInvalidRestrictiveSubject(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	result := EvaluateManagementGuard(ManagementGuardInput{Subject: domain.SignalSubjectV2{Type: "prefix", Value: "192.0.2.1/24"}, MayRestrictTraffic: true, Now: now})
	if result.ActionAllowed || result.State != ManagementGuardUnknown || !slices.Contains(result.ReasonCodes, "management_subject_unknown") {
		t.Fatalf("invalid subject bypassed recovery evaluation: %#v", result)
	}
}

func TestManagementGuardRejectsStaleEndpointRevisionAndWrongSourceFamily(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	endpoint := managementFixture(now)
	path := recoveryFixture(endpoint.ID, "198.51.100.0/24", now)
	path.ConfigurationRevision = strings.Repeat("d", 64)
	input := ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: endpoint.ResourceID}, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, EndpointKey: hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: endpoint.Bind, Port: endpoint.Port}, Management: []hostresources.ManagementEndpointV1{endpoint}, RecoveryPaths: []hostresources.RecoveryPathV1{path}, MayRestrictTraffic: true, Now: now}
	stale := EvaluateManagementGuard(input)
	if stale.ActionAllowed || !slices.Contains(stale.ReasonCodes, "fresh_recovery_path_missing") {
		t.Fatalf("stale endpoint revision became an eligible path: %#v", stale)
	}
	path.ConfigurationRevision = endpoint.ConfigurationRevision
	path.SourcePrefix = "2001:db8::/64"
	input.RecoveryPaths = []hostresources.RecoveryPathV1{path}
	wrongFamily := EvaluateManagementGuard(input)
	if wrongFamily.ActionAllowed || !slices.Contains(wrongFamily.ReasonCodes, "fresh_recovery_path_missing") {
		t.Fatalf("wrong address-family recovery became eligible: %#v", wrongFamily)
	}
}

func managementFixture(now time.Time) hostresources.ManagementEndpointV1 {
	return hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.5", Port: 443, ServiceKind: hostresources.ManagementPanel, Exposure: hostresources.EndpointIntentPublic, Owner: "panel", ResourceID: "core:panel:web", RecoveryPolicy: "fresh_independent_path_required", Purpose: "administrative_access", ConfiguredIntent: true, Source: "fixture", ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(90 * time.Second).Unix(), ConfigurationRevision: strings.Repeat("c", 64)}
}

func recoveryFixture(endpointID, sourcePrefix string, now time.Time) hostresources.RecoveryPathV1 {
	return hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:path", Kind: string(hostresources.ManagementPanel), EndpointID: endpointID, PrincipalID: "principal:hash", SourcePrefix: sourcePrefix, VerificationMethod: "fresh_panel_login", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: strings.Repeat("c", 64)}
}
