package config

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

// DownloadEndpointPeerConfig is explicit administrator credential delivery.
// Normal endpoint reads retain masked managed secrets.
func (a *Handler) DownloadEndpointPeerConfig(c *gin.Context) {
	if a.RequireScope == nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if !a.RequireScope(c, "endpointPeerConfig", "admin") {
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	var request struct {
		ID   uint   `json:"id"`
		Host string `json:"host"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.ID == 0 || request.Host == "" || len(request.Host) > 253 {
		c.JSON(http.StatusBadRequest, Envelope{Msg: "endpoint_peer_config_request_invalid"})
		return
	}
	configs, err := a.EndpointService.PeerConfigs(c.Request.Context(), request.ID, request.Host)
	if err != nil {
		c.JSON(http.StatusConflict, Envelope{Msg: "endpoint_peer_config_unavailable"})
		return
	}
	a.Audit(c, a.Actor(c), "endpoint_peer_config_delivered", "endpoints", service.AuditSeverityWarn, map[string]any{"id": request.ID, "count": len(configs)})
	c.JSON(http.StatusOK, Envelope{Success: true, Obj: configs})
}
