package sshmanagement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const evidenceProducerRevision = "a7edc2e0c98e65ec144c158337a75d28ce9669c54f58cc7153ae8010276d40ca"

func (m *Manager) ProviderID() string { return "core-ssh-management" }

func (m *Manager) RecoveryPaths(ctx context.Context, now time.Time) ([]hostresources.RecoveryPathV1, error) {
	rows, err := m.Repository.RecoveryRows(ctx, now)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.RecoveryPathV1, 0, len(rows))
	for _, row := range rows {
		var reasons []string
		if json.Unmarshal(row.ReasonCodesJSON, &reasons) != nil {
			continue
		}
		path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: row.ID, Kind: row.Kind, EndpointID: row.EndpointID,
			PrincipalID: row.PrincipalID, SourcePrefix: row.SourcePrefix, VerificationMethod: row.VerificationMethod,
			EvidenceProvider: row.EvidenceProvider, TargetOperation: row.TargetOperation, VerifiedAt: row.VerifiedAt, ExpiresAt: row.ExpiresAt,
			IndependenceClass: row.IndependenceClass, VerificationState: row.VerificationState, OperationBound: row.OperationBound,
			SingleUse: row.SingleUse, ConsumedAt: row.ConsumedAt, Revision: row.Revision, ReasonCodes: reasons,
			SourceRevision: row.SourceRevision, ConfigurationRevision: row.ConfigurationRevision, ServiceRevision: row.ServiceRevision,
			BinaryRevision: row.BinaryRevision, ProducerRevision: row.ProducerRevision}
		if row.ProducerRevision == evidenceProducerRevision && hostresources.RecoveryPathValid(path, now) {
			result = append(result, path)
		}
	}
	return result, nil
}

func (m *Manager) HandlePanelEvent(event string, fields map[string]string) error {
	if m == nil {
		return nil
	}
	now := m.now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	principal := principalID("panel", fields["user"])
	switch event {
	case "login_success":
		return m.recordPanelLogin(ctx, fields, principal, now)
	case "logout":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), principal, "panel_session_ended", now)
	case "logout_all_admins":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), "", "panel_session_generation_changed", now)
	case "admin_credentials_changed":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), "", "panel_credentials_changed", now)
	case "admin_deleted":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), principalID("panel", fields["user"]), "panel_principal_deleted", now)
	default:
		return nil
	}
}

func (m *Manager) recordPanelLogin(ctx context.Context, fields map[string]string, principal string, now time.Time) error {
	address, err := netip.ParseAddr(strings.TrimSpace(fields["ip"]))
	sessionRevision := strings.TrimSpace(fields["sessionRevision"])
	if err != nil || !validDigest(sessionRevision) || principal == "" {
		return nil
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() {
		return nil
	}
	family := hostresources.AddressFamilyIPv6
	bits := 128
	if address.Is4() {
		family = hostresources.AddressFamilyIPv4
		bits = 32
	}
	current := make([]hostresources.ManagementEndpointV1, 0, 1)
	for _, endpoint := range m.endpoints(ctx, now) {
		if endpoint.ServiceKind == hostresources.ManagementPanel && endpoint.Family == family && hostresources.ManagementEndpointCurrent(endpoint, now) {
			current = append(current, endpoint)
		}
	}
	if len(current) != 1 {
		return nil
	}
	prefix := netip.PrefixFrom(address, bits).String()
	sourceRevision := domain.Revision(struct{ Contract, SessionRevision string }{"panel-login/v1", sessionRevision})
	path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1,
		ID:   "recovery:" + domain.Revision(struct{ Kind, Endpoint, Principal, Prefix, Method string }{string(hostresources.ManagementPanel), current[0].ID, principal, prefix, "fresh_panel_login"}),
		Kind: string(hostresources.ManagementPanel), EndpointID: current[0].ID, PrincipalID: principal, SourcePrefix: prefix,
		VerificationMethod: "fresh_panel_login", EvidenceProvider: m.ProviderID(), VerifiedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxRecoveryLifetime).Unix(),
		IndependenceClass: "independent_reconnect", VerificationState: "verified", Revision: 1, SourceRevision: sourceRevision,
		ConfigurationRevision: current[0].ConfigurationRevision, ProducerRevision: evidenceProducerRevision}
	if !hostresources.RecoveryPathValid(path, now) {
		return nil
	}
	return m.Repository.UpsertRecoveryEvidence(ctx, path, now)
}

func principalID(kind, principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(kind)) + ":" + principal))
	return "principal:" + hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
