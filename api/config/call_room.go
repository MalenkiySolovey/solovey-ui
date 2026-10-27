package config

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

// DownloadCallRoom is an explicit administrator credential delivery operation,
// separate from ordinary inbound/config reads and unavailable to component scopes.
func (a *Handler) DownloadCallRoom(c *gin.Context) {
	if a.RequireScope == nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if !a.RequireScope(c, "callRoom", "admin") {
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	var request struct {
		ID         uint   `json:"id"`
		Generation string `json:"generation"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.ID == 0 || request.Generation == "" || len(request.Generation) > 128 {
		c.JSON(http.StatusBadRequest, Envelope{Msg: "call_room_request_invalid"})
		return
	}
	var room []byte
	defer func() { clear(room) }()
	err := a.ConfigService.UseCallRoom(c.Request.Context(), request.ID, request.Generation, func(value []byte) error {
		room = append([]byte(nil), value...)
		return nil
	})
	if err != nil || len(room) == 0 {
		c.JSON(http.StatusConflict, Envelope{Msg: "call_room_unavailable"})
		return
	}
	a.Audit(c, a.Actor(c), "call_room_delivered", "inbounds", service.AuditSeverityWarn, map[string]any{"id": request.ID, "generation": request.Generation})
	c.Header("Content-Disposition", "attachment; filename=call-room-"+strconv.FormatUint(uint64(request.ID), 10)+".txt")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", room)
}
