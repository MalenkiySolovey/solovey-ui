//go:build !minimal

package api

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
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
	page := parsePage(c, 100, 500)
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
	page := parsePage(c, 100, 500)
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
	page := parsePage(c, 50, 200)
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
	IPCIDR    string `json:"ipCidr"`
	Reason    string `json:"reason"`
	ExpiresAt *int64 `json:"expiresAt"`
}

type ipAllowlistView struct {
	ID        uint   `json:"id"`
	IPCIDR    string `json:"ipCidr"`
	Reason    string `json:"reason"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt int64  `json:"createdAt"`
}

func (h Handler) ipAllowlist(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	page := parsePage(c, 50, 200)
	items, total, err := h.deps.Repository.ListIPAllowlist(c.Request.Context(), page)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	views := make([]ipAllowlistView, 0, len(items))
	for _, item := range items {
		views = append(views, ipAllowlistView{item.ID, item.IPCIDR, item.Reason, item.ExpiresAt, item.CreatedBy, item.CreatedAt})
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
	prefix, err := netip.ParsePrefix(strings.TrimSpace(input.IPCIDR))
	if err != nil {
		if addr, addrErr := netip.ParseAddr(strings.TrimSpace(input.IPCIDR)); addrErr == nil {
			bits := 128
			if addr.Unmap().Is4() {
				addr, bits = addr.Unmap(), 32
			}
			prefix = netip.PrefixFrom(addr, bits)
		} else {
			writeError(c, http.StatusBadRequest, "validation_error", errors.New("ipCidr must be an IP address or CIDR prefix"))
			return
		}
	}
	prefix = prefix.Masked()
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len(input.Reason) > 256 {
		writeError(c, http.StatusBadRequest, "validation_error", errors.New("bounded reason is required"))
		return
	}
	now := time.Now().Unix()
	item := protectionrepository.IPAllowlistModel{IPCIDR: prefix.String(), Reason: input.Reason, ExpiresAt: input.ExpiresAt, CreatedBy: h.actor(c), CreatedAt: now, UpdatedAt: now}
	if err := h.deps.Repository.CreateIPAllowlist(c.Request.Context(), &item); err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_ip_allowlist_created", map[string]any{"id": item.ID, "ipCidr": item.IPCIDR})
	h.deps.JSONObj(c, ipAllowlistView{item.ID, item.IPCIDR, item.Reason, item.ExpiresAt, item.CreatedBy, item.CreatedAt}, nil)
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
