package config

import (
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) RotateSubSecret(c *gin.Context) {
	if !a.RequireScope(c, "client", "admin", "write") {
		return
	}
	clientID := c.Query("id")
	clientName, err := a.ClientService.RotateSubSecret(clientID)
	if err == nil {
		a.Audit(c, a.Actor(c), "sub_secret_rotated", "client", service.AuditSeverityWarn, map[string]any{
			"clientId": clientID,
			"client":   clientName,
		})
		realtime.Publish(realtime.TopicConfigInvalidated, nil)
	}
	a.JSONMsg(c, "rotateSubSecret", err)
}

func (a *Handler) ResetTraffic(c *gin.Context) {
	if !a.RequireScope(c, "client", "admin", "write") {
		return
	}
	clientID := c.Query("id")
	client, err := a.ClientService.ResetTraffic(clientID)
	if err != nil {
		a.JSONMsg(c, "resetTraffic", err)
		return
	}
	a.Audit(c, a.Actor(c), "client_traffic_reset", "client", service.AuditSeverityInfo, map[string]any{
		"clientId": clientID,
		"client":   client.Name,
	})
	clients, err := a.ClientService.GetWithLocalLinks("", a.Hostname(c))
	a.JSONObj(c, map[string]interface{}{"clients": clients}, err)
}
