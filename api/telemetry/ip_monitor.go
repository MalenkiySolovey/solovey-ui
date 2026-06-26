package telemetry

import (
	"strconv"

	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetClientIPHistory(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil {
		limit = 100
	}
	rows, err := ipmonitor.History(c.Param("client"), limit)
	a.JSONObj(c, rows, err)
}

func (a *Handler) ClearClientIPHistory(c *gin.Context) {
	clientName := c.Param("client")
	err := ipmonitor.Clear(clientName)
	if err == nil {
		a.Audit(c, a.LoginUser(c), "client_ip_history_cleared", "client", service.AuditSeverityWarn, map[string]any{
			"client": clientName,
		})
	}
	a.JSONMsg(c, "save", err)
}
