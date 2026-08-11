package recoverypath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const RecoveryPathLifetime = 15 * time.Minute

type Store interface {
	UpsertRecoveryPath(context.Context, protectionrepository.RecoveryPathModel) error
	InvalidateRecoveryPaths(context.Context, string, string, string) error
	InvalidateRecoveryPathsBySourceRevision(context.Context, string, string, string) error
}

type PanelWriter struct {
	Store     Store
	Endpoints func(context.Context, time.Time) []hostresources.ManagementEndpointV1
	Now       func() time.Time
}

func (w PanelWriter) Handle(event string, fields map[string]string) error {
	if w.Store == nil {
		return nil
	}
	now := time.Now().UTC()
	if w.Now != nil {
		now = w.Now().UTC()
	}
	principal := PrincipalID("panel", fields["user"])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch event {
	case "login_success":
		return w.recordLogin(ctx, fields, principal, now)
	case "logout":
		return w.Store.InvalidateRecoveryPaths(ctx, string(hostresources.ManagementPanel), principal, "panel_session_ended")
	case "logout_all_admins":
		return w.Store.InvalidateRecoveryPaths(ctx, string(hostresources.ManagementPanel), "", "panel_session_generation_changed")
	case "admin_credentials_changed":
		return w.Store.InvalidateRecoveryPaths(ctx, string(hostresources.ManagementPanel), "", "panel_credentials_changed")
	case "admin_deleted":
		return w.Store.InvalidateRecoveryPaths(ctx, string(hostresources.ManagementPanel), PrincipalID("panel", fields["user"]), "panel_principal_deleted")
	default:
		return nil
	}
}

func (w PanelWriter) recordLogin(ctx context.Context, fields map[string]string, principal string, now time.Time) error {
	address, err := netip.ParseAddr(strings.TrimSpace(fields["ip"]))
	sessionRevision := strings.TrimSpace(fields["sessionRevision"])
	if err != nil {
		return nil
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() || principal == "" || !validRevision(sessionRevision) {
		return nil
	}
	endpoints := w.Endpoints
	if endpoints == nil {
		endpoints = CurrentManagementEndpoints
	}
	current := make([]hostresources.ManagementEndpointV1, 0)
	family := hostresources.AddressFamilyIPv6
	if address.Is4() {
		family = hostresources.AddressFamilyIPv4
	}
	for _, endpoint := range endpoints(ctx, now) {
		if endpoint.ServiceKind == hostresources.ManagementPanel && endpoint.Family == family && hostresources.ManagementEndpointCurrent(endpoint) {
			current = append(current, endpoint)
		}
	}
	if len(current) != 1 {
		return nil
	}
	bits := 128
	if family == hostresources.AddressFamilyIPv4 {
		bits = 32
	}
	prefix := netip.PrefixFrom(address, bits).String()
	sourceRevision := revision(struct {
		Contract, SessionRevision string
	}{"panel-login/v1", sessionRevision})
	id := RecoveryPathID(string(hostresources.ManagementPanel), current[0].ID, principal, prefix, "fresh_panel_login")
	reasonJSON, _ := json.Marshal([]string{})
	return w.Store.UpsertRecoveryPath(ctx, protectionrepository.RecoveryPathModel{
		RecoveryPathID: id, Kind: string(hostresources.ManagementPanel), EndpointID: current[0].ID, PrincipalID: principal,
		SourcePrefix: prefix, VerificationMethod: "fresh_panel_login", VerifiedAt: now.Unix(), ExpiresAt: now.Add(RecoveryPathLifetime).Unix(),
		IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: reasonJSON,
		SourceRevision: sourceRevision, ConfigurationRevision: current[0].ConfigurationRevision,
	})
}

func validRevision(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func PrincipalID(kind, principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(kind)) + ":" + principal))
	return "principal:" + hex.EncodeToString(sum[:])
}

func RecoveryPathID(kind, endpointID, principalID, sourcePrefix, method string) string {
	return "recovery:" + revision(struct {
		Kind, EndpointID, PrincipalID, SourcePrefix, Method string
	}{kind, endpointID, principalID, sourcePrefix, method})
}

func revision(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
