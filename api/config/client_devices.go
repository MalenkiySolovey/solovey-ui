package config

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func privateDeviceResponse(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (a *Handler) GetClientDevices(c *gin.Context) {
	if a.RequireScope == nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if !a.RequireScope(c, "clientDevices", "admin", "read") {
		return
	}
	privateDeviceResponse(c)
	id, err := strconv.ParseUint(c.Query("clientId"), 10, strconv.IntSize)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, Envelope{Msg: "CLIENT_DEVICE_INVALID"})
		return
	}
	view, err := a.ClientService.WireGuardDevices(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusConflict, Envelope{Msg: "CLIENT_DEVICE_UNAVAILABLE"})
		return
	}
	c.JSON(http.StatusOK, Envelope{Success: true, Obj: view})
}

func (a *Handler) DownloadClientDeviceConfig(c *gin.Context) {
	if a.RequireScope == nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if !a.RequireScope(c, "clientDevices", "admin") {
		return
	}
	privateDeviceResponse(c)
	var request struct {
		ClientID uint   `json:"clientId"`
		DeviceID string `json:"deviceId"`
		Host     string `json:"host"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.ClientID == 0 || len(request.DeviceID) != 32 || len(request.Host) > 253 {
		c.JSON(http.StatusBadRequest, Envelope{Msg: "CLIENT_DEVICE_INVALID"})
		return
	}
	config, err := a.ClientService.WireGuardDeviceConfig(c.Request.Context(), request.ClientID, request.DeviceID, request.Host)
	if err != nil {
		c.JSON(http.StatusConflict, Envelope{Msg: "CLIENT_DEVICE_UNAVAILABLE"})
		return
	}
	a.Audit(c, a.Actor(c), "client_device_config_delivered", "client", service.AuditSeverityWarn, map[string]any{"clientId": request.ClientID, "deviceId": request.DeviceID})
	c.JSON(http.StatusOK, Envelope{Success: true, Obj: config})
}
