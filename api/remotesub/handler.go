// Package remotesub owns remote outbound subscription HTTP handlers.
package remotesub

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Service        *service.RemoteOutboundService
	RequireScope   func(*gin.Context, string, ...string) bool
	Actor          func(*gin.Context) string
	ValidateTarget func(context.Context, string) error
	JSONObj        func(*gin.Context, interface{}, error)
	JSONMsg        func(*gin.Context, string, error)
}

// Deps contains the host capabilities required by remote subscription routes.
type Deps struct {
	Service        *service.RemoteOutboundService
	RequireScope   func(*gin.Context, string, ...string) bool
	Actor          func(*gin.Context) string
	ValidateTarget func(context.Context, string) error
	JSONObj        func(*gin.Context, interface{}, error)
	JSONMsg        func(*gin.Context, string, error)
}

const remoteOutboundDefaultCheckTarget = "https://www.gstatic.com/generate_204"

// maxRemoteOutboundPayloadBytes bounds the request body for subscription/group
// JSON payloads to prevent unbounded memory use from oversized requests.
const maxRemoteOutboundPayloadBytes = 1 << 20 // 1 MiB

// RegisterRoutes mounts remote subscription endpoints on an already secured API group.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	a := &Handler{
		Service:        deps.Service,
		RequireScope:   deps.RequireScope,
		Actor:          deps.Actor,
		ValidateTarget: deps.ValidateTarget,
		JSONObj:        deps.JSONObj,
		JSONMsg:        deps.JSONMsg,
	}
	group := g.Group("/remote-outbound-subscriptions")
	group.GET("", a.GetRemoteOutboundSubscriptions)
	group.POST("/save", a.SaveRemoteOutboundSubscription)
	group.POST("/delete", a.DeleteRemoteOutboundSubscription)
	group.POST("/refresh", a.RefreshRemoteOutboundSubscription)
	group.GET("/collected", a.GetRemoteOutboundSubscriptionCollectedData)
	group.GET("/test", a.TestRemoteOutboundSubscription)
	group.GET("/test-all", a.TestRemoteOutboundSubscriptions)
	group.POST("/groups/save", a.SaveRemoteOutboundGroup)
	group.POST("/groups/bulk", a.SaveRemoteOutboundGroupBulk)
	group.POST("/groups/delete", a.DeleteRemoteOutboundGroup)
	group.POST("/groups/connections", a.SetRemoteOutboundGroupConnections)
	group.POST("/groups/outbounds", a.ToggleRemoteOutboundGroupOutbounds)
	group.POST("/connections/group", a.MoveRemoteOutboundConnectionGroup)
	group.POST("/connections/sync", a.SyncRemoteOutboundConnection)
	group.GET("/connections/test", a.TestRemoteOutboundConnection)
}

type remoteOutboundGroupConnectionsPayload struct {
	GroupId       uint   `json:"groupId"`
	ConnectionIds []uint `json:"connectionIds"`
}

type remoteOutboundBulkGroupPayload struct {
	Name string `json:"name"`
}

func (a *Handler) GetRemoteOutboundSubscriptions(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "read", "write") {
		return
	}
	subscriptions, err := a.Service.GetAll()
	a.JSONObj(c, subscriptions, err)
}

func (a *Handler) SaveRemoteOutboundSubscription(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var subscription model.RemoteOutboundSubscription
	raw, err := readRemoteOutboundPayload(c, &subscription)
	if err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	saved, err := a.Service.SaveSubscription(subscription, jsonPayloadHasKey(raw, "enabled"), a.Actor(c))
	a.JSONObj(c, saved, err)
}

func (a *Handler) DeleteRemoteOutboundSubscription(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	err := a.Service.DeleteSubscription(id, a.Actor(c))
	a.JSONMsg(c, "del", err)
}

func (a *Handler) RefreshRemoteOutboundSubscription(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	result, err := a.Service.RefreshSubscription(id, a.Actor(c))
	a.JSONObj(c, result, err)
}

func (a *Handler) GetRemoteOutboundSubscriptionCollectedData(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "read", "write") {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	result, err := a.Service.GetCollectedData(id)
	a.JSONObj(c, result, err)
}

func (a *Handler) SaveRemoteOutboundGroup(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var group model.RemoteOutboundGroup
	raw, err := readRemoteOutboundPayload(c, &group)
	if err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	saved, err := a.Service.SaveGroup(group, jsonPayloadHasKey(raw, "enabled"), a.Actor(c))
	a.JSONObj(c, saved, err)
}

func (a *Handler) SaveRemoteOutboundGroupBulk(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var payload remoteOutboundBulkGroupPayload
	if _, err := readRemoteOutboundPayload(c, &payload); err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	result, err := a.Service.SaveGroupForAllSubscriptions(payload.Name, a.Actor(c))
	a.JSONObj(c, result, err)
}

func (a *Handler) DeleteRemoteOutboundGroup(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	err := a.Service.DeleteGroup(id, a.Actor(c))
	a.JSONMsg(c, "del", err)
}

func (a *Handler) SetRemoteOutboundGroupConnections(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var payload remoteOutboundGroupConnectionsPayload
	if _, err := readRemoteOutboundPayload(c, &payload); err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	err := a.Service.SetGroupConnections(payload.GroupId, payload.ConnectionIds, a.Actor(c))
	a.JSONMsg(c, "update", err)
}

func (a *Handler) ToggleRemoteOutboundGroupOutbounds(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	groupID, ok := a.remoteOutboundIDOrFail(c, "groupId")
	if !ok {
		return
	}
	result, err := a.Service.ToggleGroupOutbounds(groupID, a.Actor(c))
	a.JSONObj(c, result, err)
}

func (a *Handler) MoveRemoteOutboundConnectionGroup(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	connectionID, ok := a.remoteOutboundIDOrFail(c, "connectionId")
	if !ok {
		return
	}
	groupID, ok := a.remoteOutboundIDOrFail(c, "groupId")
	if !ok {
		return
	}
	err := a.Service.MoveConnectionToGroup(connectionID, groupID, a.Actor(c))
	a.JSONMsg(c, "update", err)
}

func (a *Handler) SyncRemoteOutboundConnection(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	connection, err := a.Service.SyncConnectionToOutbound(id, a.Actor(c))
	a.JSONObj(c, connection, err)
}

func (a *Handler) TestRemoteOutboundConnection(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := a.remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	result, err := a.Service.CheckConnection(ctx, id, target)
	a.JSONObj(c, result, err)
}

func (a *Handler) TestRemoteOutboundSubscription(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := a.remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	id, ok := a.remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	result, err := a.Service.CheckSubscription(ctx, id, target)
	a.JSONObj(c, gin.H{"target": target, "results": result}, err)
}

func (a *Handler) TestRemoteOutboundSubscriptions(c *gin.Context) {
	if !a.RequireScope(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := a.remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	result, err := a.Service.CheckAll(ctx, target)
	a.JSONObj(c, gin.H{"target": target, "results": result}, err)
}

func (a *Handler) remoteOutboundCheckTarget(c *gin.Context) (string, bool) {
	target := strings.TrimSpace(c.DefaultQuery("target", remoteOutboundDefaultCheckTarget))
	if target == "" {
		target = remoteOutboundDefaultCheckTarget
	}
	if err := a.ValidateTarget(c.Request.Context(), target); err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return "", false
	}
	return target, true
}

func readRemoteOutboundPayload(c *gin.Context, target any) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRemoteOutboundPayloadBytes)
	if strings.Contains(c.ContentType(), "json") {
		body, err := c.GetRawData()
		if err != nil {
			return nil, err
		}
		if len(body) == 0 {
			return nil, common.NewError("empty payload")
		}
		return body, json.Unmarshal(body, target)
	}
	raw := strings.TrimSpace(c.PostForm("data"))
	if raw == "" {
		return nil, common.NewError("empty payload")
	}
	body := []byte(raw)
	return body, json.Unmarshal(body, target)
}

func jsonPayloadHasKey(raw []byte, key string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	_, ok := payload[key]
	return ok
}

func remoteOutboundID(c *gin.Context, name string) (uint, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		raw = strings.TrimSpace(c.PostForm(name))
	}
	if raw == "" {
		return 0, common.NewError(name, " is required")
	}
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		return 0, common.NewError("invalid ", name)
	}
	return uint(id), nil
}

// remoteOutboundIDOrFail parses a required ID query/form parameter and, on
// failure, writes the standard error response and reports ok=false so the
// caller can return immediately.
func (a *Handler) remoteOutboundIDOrFail(c *gin.Context, name string) (uint, bool) {
	id, err := remoteOutboundID(c, name)
	if err != nil {
		a.JSONMsg(c, "remoteOutboundSubscriptions", err)
		return 0, false
	}
	return id, true
}
