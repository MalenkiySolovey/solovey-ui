//go:build !minimal

package api

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/gin-gonic/gin"
)

type eventView struct {
	ID           uint   `json:"id"`
	ResourceID   string `json:"resourceId"`
	ResourceKind string `json:"resourceKind"`
	SourceIPCIDR string `json:"sourceIpCidr,omitempty"`
	IPFamily     *int   `json:"ipFamily,omitempty"`
	SignalKind   string `json:"signalKind"`
	ScoreDelta   int    `json:"scoreDelta"`
	Action       string `json:"action"`
	SafeMeta     any    `json:"safeMeta"`
	ObservedAt   int64  `json:"observedAt"`
}

func (h Handler) events(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 100)
	items, total, err := h.deps.Repository.ListEvents(c.Request.Context(), protectionrepository.EventFilter{PageQuery: page, ResourceID: strings.TrimSpace(c.Query("resource_id")), Kind: strings.TrimSpace(c.Query("kind")), Since: queryInt64(c, "since")})
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	views := make([]eventView, 0, len(items))
	for _, item := range items {
		var safeMeta any
		_ = decodeJSON(item.SafeMetaJSON, &safeMeta)
		views = append(views, eventView{item.ID, item.ResourceID, item.ResourceKind, item.SourceIPCIDR, item.IPFamily, item.SignalKind, item.ScoreDelta, item.Action, safeMeta, item.ObservedAt})
	}
	droppedCount := uint64(0)
	if h.deps.ObservationStatus != nil {
		worker := h.deps.ObservationStatus()
		droppedCount = worker.DroppedBus + worker.DroppedBatches
	}
	h.deps.JSONObj(c, gin.H{"items": views, "page": page.Page, "limit": page.Limit, "total": total, "droppedCount": droppedCount}, nil)
}

func (h Handler) clearEvents(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	resourceID := strings.TrimSpace(c.Query("resource_id"))
	deleted, err := h.deps.Repository.ClearEvents(c.Request.Context(), resourceID)
	if err == nil {
		h.audit(c, "server_protection_events_cleared", map[string]any{"resourceId": resourceID, "deleted": deleted})
	}
	h.deps.JSONObj(c, gin.H{"deleted": deleted}, err)
}

type graylistView struct {
	ID         uint   `json:"id"`
	ResourceID string `json:"resourceId"`
	IPCIDR     string `json:"ipCidr"`
	IPFamily   int    `json:"ipFamily"`
	Score      int    `json:"score"`
	Reason     string `json:"reason"`
	LastSignal string `json:"lastSignal"`
	ExpiresAt  int64  `json:"expiresAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func (h Handler) graylist(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 100)
	items, total, err := h.deps.Repository.ListGraylist(c.Request.Context(), protectionrepository.GraylistFilter{PageQuery: page, ResourceID: strings.TrimSpace(c.Query("resource_id")), Family: queryInt(c, "family")})
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	views := make([]graylistView, 0, len(items))
	for _, item := range items {
		views = append(views, graylistView{item.ID, item.ResourceID, item.IPCIDR, item.IPFamily, item.Score, item.Reason, item.LastSignal, item.ExpiresAt, item.UpdatedAt})
	}
	h.deps.JSONObj(c, gin.H{"items": views, "page": page.Page, "limit": page.Limit, "total": total}, nil)
}

func (h Handler) clearGraylist(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	resourceID := strings.TrimSpace(c.Query("resource_id"))
	deleted, err := h.deps.Repository.ClearGraylist(c.Request.Context(), resourceID)
	if err == nil {
		h.audit(c, "server_protection_graylist_cleared", map[string]any{"resourceId": resourceID, "deleted": deleted})
	}
	h.deps.JSONObj(c, gin.H{"deleted": deleted}, err)
}

type portAllowlistInput struct {
	Protocol  string `json:"protocol"`
	Listen    string `json:"listen"`
	PortStart int    `json:"portStart"`
	PortEnd   int    `json:"portEnd"`
	Reason    string `json:"reason"`
	ExpiresAt *int64 `json:"expiresAt"`
}

type portAllowlistView struct {
	ID        uint   `json:"id"`
	Protocol  string `json:"protocol"`
	Listen    string `json:"listen"`
	PortStart int    `json:"portStart"`
	PortEnd   int    `json:"portEnd"`
	Reason    string `json:"reason"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt int64  `json:"createdAt"`
}

func (h Handler) portAllowlist(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 50)
	items, total, err := h.deps.Repository.ListPortAllowlist(c.Request.Context(), page, strings.ToLower(strings.TrimSpace(c.Query("protocol"))))
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	views := make([]portAllowlistView, 0, len(items))
	for _, item := range items {
		views = append(views, portAllowlistView{item.ID, item.Protocol, item.Listen, item.PortStart, item.PortEnd, item.Reason, item.ExpiresAt, item.CreatedBy, item.CreatedAt})
	}
	h.deps.JSONObj(c, gin.H{"items": views, "page": page.Page, "limit": page.Limit, "total": total}, nil)
}

func (h Handler) createPortAllowlist(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input portAllowlistInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	input.Protocol = strings.ToLower(strings.TrimSpace(input.Protocol))
	input.Listen = hostresources.NormalizeListen(input.Listen).Value
	input.Reason = strings.TrimSpace(input.Reason)
	if (input.Protocol != "tcp" && input.Protocol != "udp") || input.PortStart < 1 || input.PortEnd < input.PortStart || input.PortEnd > 65535 || input.Reason == "" || len(input.Reason) > 256 {
		writeError(c, http.StatusBadRequest, "validation_error", errors.New("protocol, port range and bounded reason are required"))
		return
	}
	now := time.Now().Unix()
	item := protectionrepository.PortAllowlistModel{Protocol: input.Protocol, Listen: input.Listen, PortStart: input.PortStart, PortEnd: input.PortEnd, Reason: input.Reason, ExpiresAt: input.ExpiresAt, CreatedBy: h.actor(c), CreatedAt: now, UpdatedAt: now}
	if err := h.deps.Repository.CreatePortAllowlist(c.Request.Context(), &item); err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_port_allowlist_created", map[string]any{"id": item.ID, "protocol": item.Protocol, "portStart": item.PortStart, "portEnd": item.PortEnd})
	h.deps.JSONObj(c, portAllowlistView{item.ID, item.Protocol, item.Listen, item.PortStart, item.PortEnd, item.Reason, item.ExpiresAt, item.CreatedBy, item.CreatedAt}, nil)
}

func (h Handler) deletePortAllowlist(c *gin.Context) {
	h.deleteAllowlist(c, "port")
}

type ipAllowlistInput struct {
	IPCIDR                 string `json:"ipCidr"`
	Reason                 string `json:"reason"`
	ExpiresAt              *int64 `json:"expiresAt"`
	BroadScopeAcknowledged bool   `json:"broadScopeAcknowledged"`
	Confirmation           string `json:"confirmation"`
}

type ipAllowlistView struct {
	ID                     uint   `json:"id"`
	IPCIDR                 string `json:"ipCidr"`
	Reason                 string `json:"reason"`
	ExpiresAt              *int64 `json:"expiresAt,omitempty"`
	BroadScopeAcknowledged bool   `json:"broadScopeAcknowledged"`
	CreatedBy              string `json:"createdBy"`
	CreatedAt              int64  `json:"createdAt"`
}

func (h Handler) ipAllowlist(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 50)
	items, total, err := h.deps.Repository.ListIPAllowlist(c.Request.Context(), page)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	views := make([]ipAllowlistView, 0, len(items))
	for _, item := range items {
		views = append(views, ipAllowlistView{item.ID, item.IPCIDR, item.Reason, item.ExpiresAt, item.BroadScopeAcknowledged, item.CreatedBy, item.CreatedAt})
	}
	h.deps.JSONObj(c, gin.H{"items": views, "page": page.Page, "limit": page.Limit, "total": total}, nil)
}

func (h Handler) createIPAllowlist(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input ipAllowlistInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	validated, err := protectionpolicy.ValidateTrustedSource(input.IPCIDR, input.BroadScopeAcknowledged, input.Confirmation)
	if err != nil {
		code := "validation_error"
		if errors.Is(err, protectionpolicy.ErrTrustedSourceBroadConfirmation) {
			code = "confirmation_required"
		}
		writeError(c, http.StatusBadRequest, code, err)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len(input.Reason) > 256 {
		writeError(c, http.StatusBadRequest, "validation_error", errors.New("bounded reason is required"))
		return
	}
	now := time.Now().Unix()
	if input.ExpiresAt != nil && *input.ExpiresAt <= now {
		writeError(c, http.StatusBadRequest, "validation_error", errors.New("expiresAt must be in the future"))
		return
	}
	exists, err := h.deps.Repository.IPAllowlistExists(c.Request.Context(), validated.Prefix.String())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	if exists {
		writeError(c, http.StatusConflict, "duplicate_trusted_source", errors.New("trusted source already exists"))
		return
	}
	item := protectionrepository.IPAllowlistModel{IPCIDR: validated.Prefix.String(), Reason: input.Reason, ExpiresAt: input.ExpiresAt,
		BroadScopeAcknowledged: validated.Broad && input.BroadScopeAcknowledged, CreatedBy: h.actor(c), CreatedAt: now, UpdatedAt: now}
	if err := h.deps.Repository.CreateIPAllowlist(c.Request.Context(), &item); err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_ip_allowlist_created", map[string]any{"id": item.ID, "ipCidr": item.IPCIDR})
	h.deps.JSONObj(c, ipAllowlistView{item.ID, item.IPCIDR, item.Reason, item.ExpiresAt, item.BroadScopeAcknowledged, item.CreatedBy, item.CreatedAt}, nil)
}

type trustedSourceProposalView struct {
	Available       bool   `json:"available"`
	IPCIDR          string `json:"ipCidr,omitempty"`
	Family          string `json:"family,omitempty"`
	Provenance      string `json:"provenance,omitempty"`
	BindingRevision string `json:"bindingRevision,omitempty"`
	ConfigRevision  string `json:"configRevision,omitempty"`
	ReasonCode      string `json:"reasonCode,omitempty"`
}

func (h Handler) ipAllowlistProposal(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	identity := clientidentity.ResolveRequest(c.Request)
	if h.deps.ClientIdentity != nil {
		identity = h.deps.ClientIdentity(c)
	}
	if !clientidentity.ValidForSecurityGrant(identity) {
		h.deps.JSONObj(c, trustedSourceProposalView{Available: false, ReasonCode: "client_identity_untrusted"}, nil)
		return
	}
	address, err := netip.ParseAddr(identity.ClientIP)
	if err != nil {
		h.deps.JSONObj(c, trustedSourceProposalView{Available: false, ReasonCode: "client_identity_invalid"}, nil)
		return
	}
	address = address.Unmap()
	bits := 128
	if address.Is4() {
		bits = 32
	}
	validated, err := protectionpolicy.ValidateTrustedSource(netip.PrefixFrom(address, bits).String(), false, "")
	if err != nil || !digestRevision(clientidentity.BindingRevision(identity)) {
		h.deps.JSONObj(c, trustedSourceProposalView{Available: false, ReasonCode: "client_identity_unusable"}, nil)
		return
	}
	h.deps.JSONObj(c, trustedSourceProposalView{Available: true, IPCIDR: validated.Prefix.String(), Family: validated.Family,
		Provenance: identity.Provenance, BindingRevision: clientidentity.BindingRevision(identity), ConfigRevision: identity.ConfigRevision}, nil)
}

func digestRevision(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (h Handler) deleteIPAllowlist(c *gin.Context) {
	h.deleteAllowlist(c, "ip")
}

func (h Handler) deleteAllowlist(c *gin.Context, kind string) {
	if !h.writeAllowed(c) {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var err error
	if kind == "port" {
		err = h.deps.Repository.DeletePortAllowlist(c.Request.Context(), id)
	} else {
		err = h.deps.Repository.DeleteIPAllowlist(c.Request.Context(), id)
	}
	if err == nil {
		h.audit(c, "server_protection_"+kind+"_allowlist_deleted", map[string]any{"id": id})
	}
	h.deps.JSONMsg(c, "deleted", err)
}
