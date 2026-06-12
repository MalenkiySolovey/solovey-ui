package api

import (
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) RotateSubSecret(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "client", "admin", "write") {
		return
	}
	clientID := c.Query("id")
	clientName, err := a.ClientService.RotateSubSecret(clientID)
	if err == nil {
		a.recordAudit(c, requestActor(c), "sub_secret_rotated", "client", service.AuditSeverityWarn, map[string]any{
			"clientId": clientID,
			"client":   clientName,
		})
		realtime.Publish(realtime.TopicConfigInvalidated, nil)
	}
	jsonMsg(c, "rotateSubSecret", err)
}
