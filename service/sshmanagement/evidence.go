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
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const evidenceProducerRevision = "3ec512aa55e4e3b88cafb554cd69c4febd25aca2c85e283181710c71146c72b3"

type RecoveryDispositionV1 struct {
	Code       domain.ReasonCode `json:"code"`
	ObservedAt int64             `json:"observedAt"`
}

func (m *Manager) ProviderID() string { return "core-ssh-management" }

func (m *Manager) RecoveryPaths(ctx context.Context, now time.Time) ([]hostresources.RecoveryPathV1, error) {
	rows, err := m.Repository.RecoveryRows(ctx, now)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.RecoveryPathV1, 0, len(rows))
	currentIdentityConfig := clientidentity.ConfigFromEnvironment().Revision
	bindingStale := false
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
			BinaryRevision: row.BinaryRevision, ProducerRevision: row.ProducerRevision,
			ClientIdentityBindingRevision: row.ClientIdentityBindingRevision, ClientIdentityConfigRevision: row.ClientIdentityConfigRevision,
			ClientIdentityProvenance: row.ClientIdentityProvenance}
		if row.ProducerRevision != evidenceProducerRevision || !hostresources.RecoveryPathValid(path, now) {
			continue
		}
		if path.VerificationMethod == "fresh_panel_login" {
			if path.ClientIdentityConfigRevision != currentIdentityConfig {
				bindingStale = true
				continue
			}
			if !validDigest(path.ClientIdentityBindingRevision) ||
				(path.ClientIdentityProvenance != clientidentity.ProvenanceDirect && path.ClientIdentityProvenance != clientidentity.ProvenanceTrustedXFF) {
				continue
			}
		}
		result = append(result, path)
	}
	if bindingStale && len(result) == 0 {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryBindingStale, now)
	} else {
		m.clearRecoveryDisposition(domain.ReasonRecoveryBindingStale)
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
	case "logout":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), principal, "panel_session_ended", now)
	case "logout_all_admins":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), "", "panel_session_generation_changed", now)
	case "admin_credentials_changed":
		err := m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), "", "panel_credentials_changed", now)
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryReauthRequired, now)
		return err
	case "session_reestablished_after_credential_change":
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryReauthRequired, now)
		return nil
	case "admin_deleted":
		return m.Repository.InvalidateRecoveryEvidence(ctx, string(hostresources.ManagementPanel), principalID("panel", fields["user"]), "panel_principal_deleted", now)
	default:
		return nil
	}
}

func (m *Manager) HandlePanelAuthenticationEvent(event, user, sessionRevision string, identity clientidentity.V1) error {
	if m == nil || event != "login_success" {
		return nil
	}
	now := m.now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return m.recordPanelLogin(ctx, strings.TrimSpace(sessionRevision), principalID("panel", user), identity, now)
}

func (m *Manager) recordPanelLogin(ctx context.Context, sessionRevision, principal string, identity clientidentity.V1, now time.Time) error {
	if identity.Provenance == clientidentity.ProvenanceUnknown {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryIdentityUnknown, now)
		return nil
	}
	if !clientidentity.ValidForSecurityGrant(identity) || !validDigest(sessionRevision) || principal == "" {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryIdentityInvalid, now)
		return nil
	}
	address, err := netip.ParseAddr(strings.TrimSpace(identity.ClientIP))
	if err != nil {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryIdentityInvalid, now)
		return nil
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryIdentityInvalid, now)
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
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryEndpointAmbiguous, now)
		return nil
	}
	prefix := netip.PrefixFrom(address, bits).String()
	bindingRevision := clientidentity.BindingRevision(identity)
	sourceRevision := domain.Revision(struct{ Contract, SessionRevision, IdentityBinding string }{"panel-login/v2", sessionRevision, bindingRevision})
	path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1,
		ID:   "recovery:" + domain.Revision(struct{ Kind, Endpoint, Principal, Prefix, Method string }{string(hostresources.ManagementPanel), current[0].ID, principal, prefix, "fresh_panel_login"}),
		Kind: string(hostresources.ManagementPanel), EndpointID: current[0].ID, PrincipalID: principal, SourcePrefix: prefix,
		VerificationMethod: "fresh_panel_login", EvidenceProvider: m.ProviderID(), VerifiedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxRecoveryLifetime).Unix(),
		IndependenceClass: "independent_reconnect", VerificationState: "verified", Revision: 1, SourceRevision: sourceRevision,
		ConfigurationRevision: current[0].ConfigurationRevision, ProducerRevision: evidenceProducerRevision,
		ClientIdentityBindingRevision: bindingRevision, ClientIdentityConfigRevision: identity.ConfigRevision,
		ClientIdentityProvenance: identity.Provenance}
	if !hostresources.RecoveryPathValid(path, now) {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryEvidenceInvalid, now)
		return nil
	}
	if err := m.Repository.UpsertRecoveryEvidence(ctx, path, now); err != nil {
		m.setRecoveryDisposition(ctx, domain.ReasonRecoveryPersistFailed, now)
		return err
	}
	m.setRecoveryDisposition(ctx, "", now)
	return nil
}

func (m *Manager) RecoveryDisposition() *RecoveryDispositionV1 {
	if m == nil {
		return nil
	}
	m.recoveryMu.RLock()
	defer m.recoveryMu.RUnlock()
	if m.recoveryDisposition.ObservedAt == 0 {
		return nil
	}
	value := m.recoveryDisposition
	return &value
}

func (m *Manager) setRecoveryDisposition(ctx context.Context, code domain.ReasonCode, now time.Time) {
	m.recoveryMu.Lock()
	changed := m.recoveryDisposition.Code != code
	if !changed && m.recoveryDisposition.ObservedAt != 0 {
		m.recoveryMu.Unlock()
		return
	}
	m.recoveryDisposition = RecoveryDispositionV1{Code: code, ObservedAt: now.UTC().Unix()}
	m.recoveryMu.Unlock()
	if m.Audit != nil {
		event := "recovery_evidence_persisted"
		if code != "" {
			event = "recovery_evidence_rejected"
		}
		m.Audit(ctx, AuditEventV1{Event: event, ReasonCode: code})
	}
}

func (m *Manager) clearRecoveryDisposition(code domain.ReasonCode) {
	if m == nil {
		return
	}
	m.recoveryMu.Lock()
	if m.recoveryDisposition.Code == code {
		m.recoveryDisposition = RecoveryDispositionV1{}
	}
	m.recoveryMu.Unlock()
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
