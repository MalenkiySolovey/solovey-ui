//go:build !minimal

package api

import (
	"encoding/json"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func (h Handler) hostSurfaces(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	snapshot := hostfacts.CurrentSnapshot()
	if queryBool(c, "refresh") || snapshot.GeneratedAt == 0 {
		snapshot = hostfacts.Reconcile(c.Request.Context())
	}
	now := time.Now().UTC()
	classification := strings.TrimSpace(c.Query("classification"))
	filtered := make([]hostfacts.HostSurfaceFactV1, 0, len(snapshot.Facts))
	for _, fact := range snapshot.Facts {
		fact.Stale = fact.IsStale(now)
		if fact.Stale {
			fact.ReasonCodes = appendReason(fact.ReasonCodes, "stale")
		}
		if classification != "" && string(fact.Classification) != classification {
			continue
		}
		filtered = append(filtered, fact)
	}
	page := parsePage(c, 100, 500)
	items, total := paginate(filtered, page)
	h.deps.JSONObj(c, gin.H{"items": items, "page": page.Page, "limit": page.Limit, "total": total, "generatedAt": snapshot.GeneratedAt, "truncated": snapshot.Truncated, "reasonCodes": snapshot.ReasonCodes}, nil)
}

func (h Handler) targetCapabilities(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	now := time.Now().UTC()
	snapshotV1 := neutralfallback.Default.Snapshot(c.Request.Context(), now)
	snapshotV2 := neutralfallback.Default.SnapshotV2(c.Request.Context(), now)
	page := parsePage(c, 100, 500)
	type legacyTargetView struct {
		Identity    neutralfallback.TargetIdentity `json:"identity"`
		EndpointID  string                         `json:"endpointId"`
		Readiness   neutralfallback.Readiness      `json:"readiness"`
		ObservedAt  int64                          `json:"observedAt"`
		ExpiresAt   int64                          `json:"expiresAt"`
		ReasonCodes []string                       `json:"reasonCodes,omitempty"`
		Legacy      bool                           `json:"legacy"`
		Actionable  bool                           `json:"actionable"`
	}
	legacy := make([]legacyTargetView, 0, len(snapshotV1.Targets))
	for _, target := range snapshotV1.Targets {
		legacy = append(legacy, legacyTargetView{
			Identity: target.Identity, EndpointID: target.Endpoint.EndpointID, Readiness: target.Readiness,
			ObservedAt: target.ObservedAt, ExpiresAt: target.ExpiresAt,
			ReasonCodes: boundedStrings(append(target.ReasonCodes, "legacy_target_v1_non_actionable"), 32), Legacy: true, Actionable: false,
		})
	}
	items, total := paginate(legacy, page)
	targetsV2 := make([]nativeTargetSummary, 0, len(snapshotV2.Targets))
	for _, target := range snapshotV2.Targets {
		targetsV2 = append(targetsV2, projectNativeTarget(target, now))
	}
	pagedV2, totalV2 := paginate(targetsV2, page)
	leaseRows, leaseTotal, err := h.deps.Repository.ListLeases(c.Request.Context(), page)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	type leaseView struct {
		LeaseID                 string   `json:"leaseId"`
		ProviderID              string   `json:"providerId"`
		TargetID                string   `json:"targetId"`
		PublishRevision         string   `json:"publishRevision"`
		ApprovedLocalEndpointID string   `json:"approvedLocalEndpointId"`
		ProviderHealthRevision  string   `json:"providerHealthRevision"`
		IssuedAt                int64    `json:"issuedAt"`
		RenewedAt               int64    `json:"renewedAt"`
		ExpiresAt               int64    `json:"expiresAt"`
		ReleasedAt              int64    `json:"releasedAt,omitempty"`
		State                   string   `json:"state"`
		ReasonCodes             []string `json:"reasonCodes,omitempty"`
	}
	leases := make([]leaseView, 0, len(leaseRows))
	invalidLeaseRecords := 0
	for _, row := range leaseRows {
		var reasons []string
		if json.Unmarshal(row.ReasonCodesJSON, &reasons) != nil {
			invalidLeaseRecords++
			continue
		}
		lease := neutralfallback.ReferenceLeaseV1{
			Schema: row.Schema, LeaseID: row.LeaseID, HolderID: row.HolderID,
			ProviderID: row.ProviderID, TargetID: row.TargetID, PublishRevision: row.PublishRevision,
			ContentDigest: row.ContentDigest, ApprovedLocalEndpointID: row.ApprovedLocalEndpointID,
			ProviderHealthRevision: row.ProviderHealthRevision, IssuedAt: row.IssuedAt,
			RenewedAt: row.RenewedAt, ExpiresAt: row.ExpiresAt, ReleasedAt: row.ReleasedAt,
			State: row.State, ReasonCodes: reasons,
		}
		if lease.Validate(now) != nil {
			invalidLeaseRecords++
			continue
		}
		leases = append(leases, leaseView{
			LeaseID: lease.LeaseID, ProviderID: lease.ProviderID, TargetID: lease.TargetID,
			PublishRevision: lease.PublishRevision, ApprovedLocalEndpointID: lease.ApprovedLocalEndpointID,
			ProviderHealthRevision: lease.ProviderHealthRevision, IssuedAt: lease.IssuedAt,
			RenewedAt: lease.RenewedAt, ExpiresAt: lease.ExpiresAt, ReleasedAt: lease.ReleasedAt,
			State: lease.State, ReasonCodes: boundedStrings(lease.ReasonCodes, 32),
		})
	}
	reservations, err := neutralfallback.Default.ListReservationsV2(c.Request.Context(), neutralfallback.ListReservationsQueryV1{Limit: neutralfallback.MaxReservationListPageV2})
	if err != nil {
		writeNativeError(c, err)
		return
	}
	type reservationView struct {
		ProviderID string `json:"providerId"`
		TargetID   string `json:"targetId"`
		State      string `json:"state"`
		Revision   string `json:"revision"`
		ExpiresAt  int64  `json:"expiresAt"`
		Fresh      bool   `json:"fresh"`
	}
	reservationViews := make([]reservationView, 0, len(reservations.Reservations))
	for _, reservation := range reservations.Reservations {
		status := reservation.Status(now)
		reservationViews = append(reservationViews, reservationView{
			ProviderID: reservation.ExactTargetReference.ProviderID, TargetID: reservation.ExactTargetReference.TargetID,
			State: string(status.EffectiveState), Revision: reservation.ReservationRevision, ExpiresAt: reservation.FreshnessExpiresAt, Fresh: status.Fresh,
		})
	}
	h.deps.JSONObj(c, gin.H{
		"items": items, "targetsV2": pagedV2, "page": page.Page, "limit": page.Limit, "total": total, "totalV2": totalV2,
		"generatedAt": snapshotV2.GeneratedAt, "reasonCodes": boundedStrings(append(snapshotV1.ReasonCodes, snapshotV2.ReasonCodes...), 32),
		"leases": leases, "leaseTotal": leaseTotal, "invalidLeaseRecords": invalidLeaseRecords,
		"reservations": reservationViews, "reservationsTruncated": reservations.Truncated,
	}, nil)
}

func (h Handler) signalsV2(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 100, 500)
	rows, total, err := h.deps.Repository.ListSignalsV2(c.Request.Context(), protectionrepository.ContractFilter{PageQuery: page, Scope: strings.TrimSpace(c.Query("scope")), Kind: strings.TrimSpace(c.Query("kind")), ResourceID: strings.TrimSpace(c.Query("resource_id"))})
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	items := make([]domain.ProtectionSignalV2, 0, len(rows))
	invalidRecords := 0
	now := time.Now().UTC()
	for _, row := range rows {
		var item domain.ProtectionSignalV2
		if json.Unmarshal(row.ContractJSON, &item) != nil || !signalRowMatches(row, item) || item.Validate(now) != nil {
			invalidRecords++
			continue
		}
		if !item.ExpiresAt.After(now) {
			item.ReasonCodes = appendReason(item.ReasonCodes, domain.ReasonStale)
		}
		items = append(items, item)
	}
	h.deps.JSONObj(c, gin.H{"items": items, "page": page.Page, "limit": page.Limit, "total": total, "invalidRecords": invalidRecords, "contract": "bounded_scoped_record_only"}, nil)
}

func (h Handler) decisionsV2(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 100, 500)
	rows, total, err := h.deps.Repository.ListDecisionsV2(c.Request.Context(), protectionrepository.ContractFilter{PageQuery: page, Scope: strings.TrimSpace(c.Query("scope"))})
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	items := make([]domain.ProtectionDecisionV2, 0, len(rows))
	invalidRecords := 0
	now := time.Now().UTC()
	for _, row := range rows {
		var item domain.ProtectionDecisionV2
		if json.Unmarshal(row.ContractJSON, &item) != nil || !decisionRowMatches(row, item) || item.Validate(now) != nil {
			invalidRecords++
			continue
		}
		if !item.ExpiresAt.After(now) {
			item.State = domain.DecisionExpired
			item.ReasonCodes = appendReason(item.ReasonCodes, domain.ReasonStale)
		}
		items = append(items, item)
	}
	h.deps.JSONObj(c, gin.H{"items": items, "page": page.Page, "limit": page.Limit, "total": total, "invalidRecords": invalidRecords, "actionability": "resolver_preview_only", "implementedActions": []string{string(domain.IntentSoftGraylist), string(domain.IntentRateLimit), string(domain.IntentTemporaryQuarantine), string(domain.IntentTemporaryBlock)}, "actualStatus": "NOT_APPLIED"}, nil)
}

func (h Handler) posture(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	now := time.Now().UTC()
	inventory := protectionresources.Snapshot(c.Request.Context(), false)
	surfaces := hostfacts.CurrentSnapshot()
	management := managementregistry.Endpoints(inventory.Resources, surfaces, now)
	evidence := managementregistry.RecoveryEvidence(c.Request.Context(), now)
	pathContracts := make([]hostresources.RecoveryPathV1, 0, len(evidence.Paths))
	invalidPaths := len(evidence.ReasonCodes)
	for _, value := range evidence.Paths {
		value = managementregistry.Effective(value, management, now)
		if !hostresources.RecoveryPathValid(value, now) {
			invalidPaths++
			continue
		}
		pathContracts = append(pathContracts, value)
	}
	h.deps.JSONObj(c, gin.H{"managementEndpoints": management, "recoveryPaths": pathContracts, "recoveryTotal": len(pathContracts), "invalidRecoveryRecords": invalidPaths, "capabilities": []gin.H{{"kind": "PANEL", "state": "neutral_verified_login_writer"}, {"kind": "SSH", "state": "production_broker_adapter"}, {"kind": "SUBSCRIPTION_ADMIN", "state": "contract_only_no_observed_endpoint"}, {"kind": "OTHER_ADMIN", "state": "contract_only_no_observed_endpoint"}}, "recoveryState": recoveryState(pathContracts, management, now), "implemented": "neutral_recovery_contract_consumer_and_broker_adapter", "planned": "external_acceptance_pending"}, nil)
}

func recoveryState(paths []hostresources.RecoveryPathV1, management []hostresources.ManagementEndpointV1, now time.Time) string {
	current := make(map[string]hostresources.ManagementServiceKind, len(management))
	for _, endpoint := range management {
		if hostresources.ManagementEndpointCurrent(endpoint, now) {
			current[endpoint.ID] = endpoint.ServiceKind
		}
	}
	for _, path := range paths {
		kind, exists := current[path.EndpointID]
		if exists && strings.EqualFold(path.Kind, string(kind)) && hostresources.RecoveryPathFresh(path, now) {
			return "fresh_independent_path_present"
		}
	}
	return "recovery_path_unproven"
}

func signalRowMatches(row protectionrepository.ProtectionSignalV2Model, item domain.ProtectionSignalV2) bool {
	return row.SignalID == item.SignalID && row.Schema == item.Schema && row.SourceID == item.Source.SourceID && row.SourceClass == item.Source.SourceClass && row.Category == string(item.Category) && row.Kind == item.Kind && row.KnownKind == item.KnownKind && row.SubjectType == item.Subject.Type && row.SubjectValue == item.Subject.Value && row.Scope == string(item.Scope.Scope) && row.TargetResourceID == item.Scope.TargetResourceID && row.ObservedAt == item.ObservedAt.Unix() && row.ExpiresAt == item.ExpiresAt.Unix() && row.ConfidenceBP == item.ConfidenceBP && row.PolicyRevision == item.Provenance.PolicyRevision
}

func decisionRowMatches(row protectionrepository.ProtectionDecisionV2Model, item domain.ProtectionDecisionV2) bool {
	return row.DecisionID == item.DecisionID && row.Schema == item.Schema && row.PolicyRevision == item.PolicyRevision && row.SubjectType == item.Subject.Type && row.SubjectValue == item.Subject.Value && row.Scope == string(item.Scope.Scope) && row.RequestedIntent == string(item.RequestedIntent) && row.ResolvedIntent == string(item.CapabilityResolution.ResolvedIntent) && row.ActionImplemented == item.CapabilityResolution.Implemented && row.State == string(item.State) && row.CreatedAt == item.CreatedAt.Unix() && row.ExpiresAt == item.ExpiresAt.Unix()
}

func paginate[T any](values []T, page protectionrepository.PageQuery) ([]T, int) {
	total := len(values)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.Limit
	if end > total {
		end = total
	}
	return values[start:end], total
}
func appendReason(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if len(values) >= 32 {
		result := append([]string(nil), values...)
		result[len(result)-1] = value
		return result
	}
	return append(values, value)
}
