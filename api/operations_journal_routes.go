package api

import (
	"net/http"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
)

func (a *APIHandler) operationJournal(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	limit := 100
	var cursor operationcoordination.JournalCursor
	for key, target := range map[string]*uint64{"updateAfter": &cursor.Update, "dataAfter": &cursor.Data} {
		if raw := c.Query(key); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 63)
			if err != nil {
				securityBadRequest(c, "invalid journal cursor")
				return
			}
			*target = value
		}
	}
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > operationcoordination.MaxJournalPage {
			securityBadRequest(c, "invalid journal limit")
			return
		}
		limit = value
	}
	update := a.Update
	if update == nil {
		update = updateservice.SharedLifecycle()
	}
	data := a.DataLifecycle
	if data == nil {
		data = datalifecycle.Shared()
	}
	page, err := operationcoordination.ReadJournal(c.Request.Context(), update.ReadJournal, data.ReadJournal, cursor, limit)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, Msg{Success: false, Msg: "operation_journal_unavailable"})
		return
	}
	jsonObj(c, page, nil)
}
