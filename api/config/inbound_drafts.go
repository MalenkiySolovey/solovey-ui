package config

import (
	"strconv"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	inbounddrafts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/drafts"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func (a *Handler) ApplyInboundDraft(c *gin.Context) {
	if !a.RequireScope(c, "inboundDrafts", "admin", "write") {
		return
	}
	id, err := inboundDraftID(c)
	if err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
		return
	}
	if err := inbounddrafts.MarkApplied(dbsqlite.DB(), id, 0); err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
		return
	}
	a.Audit(c, a.Actor(c), "apply_inbound_draft", "inboundDrafts", service.AuditSeverityInfo, map[string]any{"id": id})
	if err := a.ReloadPartialData(c, []string{"inboundDrafts"}); err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
	}
}

func (a *Handler) DiscardInboundDraft(c *gin.Context) {
	if !a.RequireScope(c, "inboundDrafts", "admin", "write") {
		return
	}
	id, err := inboundDraftID(c)
	if err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
		return
	}
	if err := inbounddrafts.MarkDiscarded(dbsqlite.DB(), id, 0); err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
		return
	}
	a.Audit(c, a.Actor(c), "discard_inbound_draft", "inboundDrafts", service.AuditSeverityInfo, map[string]any{"id": id})
	if err := a.ReloadPartialData(c, []string{"inboundDrafts"}); err != nil {
		a.JSONMsg(c, "inboundDrafts", err)
	}
}

func inboundDraftID(c *gin.Context) (uint, error) {
	raw := c.Param("id")
	id, err := strconv.ParseUint(raw, 10, 0)
	if err != nil || id == 0 {
		return 0, strconv.ErrSyntax
	}
	return uint(id), nil
}
