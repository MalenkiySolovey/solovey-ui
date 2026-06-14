package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

const remoteOutboundDefaultCheckTarget = "https://www.gstatic.com/generate_204"

// maxRemoteOutboundPayloadBytes bounds the request body for subscription/group
// JSON payloads to prevent unbounded memory use from oversized requests.
const maxRemoteOutboundPayloadBytes = 1 << 20 // 1 MiB

func registerRemoteOutboundSubscriptionRoutes(g *gin.RouterGroup, a *ApiService) {
	group := g.Group("/remote-outbound-subscriptions")
	group.GET("", a.GetRemoteOutboundSubscriptions)
	group.POST("/save", a.SaveRemoteOutboundSubscription)
	group.POST("/delete", a.DeleteRemoteOutboundSubscription)
	group.POST("/refresh", a.RefreshRemoteOutboundSubscription)
	group.GET("/test", a.TestRemoteOutboundSubscription)
	group.GET("/test-all", a.TestRemoteOutboundSubscriptions)
	group.POST("/groups/save", a.SaveRemoteOutboundGroup)
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

func (a *ApiService) GetRemoteOutboundSubscriptions(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "read", "write") {
		return
	}
	subscriptions, err := a.RemoteOutboundService.GetAll()
	jsonObj(c, subscriptions, err)
}

func (a *ApiService) SaveRemoteOutboundSubscription(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var subscription model.RemoteOutboundSubscription
	raw, err := readRemoteOutboundPayload(c, &subscription)
	if err != nil {
		jsonMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	saved, err := a.RemoteOutboundService.SaveSubscription(subscription, jsonPayloadHasKey(raw, "enabled"), requestActor(c))
	jsonObj(c, saved, err)
}

func (a *ApiService) DeleteRemoteOutboundSubscription(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	err := a.RemoteOutboundService.DeleteSubscription(id, requestActor(c))
	jsonMsg(c, "del", err)
}

func (a *ApiService) RefreshRemoteOutboundSubscription(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	result, err := a.RemoteOutboundService.RefreshSubscription(id, requestActor(c))
	jsonObj(c, result, err)
}

func (a *ApiService) SaveRemoteOutboundGroup(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var group model.RemoteOutboundGroup
	raw, err := readRemoteOutboundPayload(c, &group)
	if err != nil {
		jsonMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	saved, err := a.RemoteOutboundService.SaveGroup(group, jsonPayloadHasKey(raw, "enabled"), requestActor(c))
	jsonObj(c, saved, err)
}

func (a *ApiService) DeleteRemoteOutboundGroup(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	err := a.RemoteOutboundService.DeleteGroup(id, requestActor(c))
	jsonMsg(c, "del", err)
}

func (a *ApiService) SetRemoteOutboundGroupConnections(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	var payload remoteOutboundGroupConnectionsPayload
	if _, err := readRemoteOutboundPayload(c, &payload); err != nil {
		jsonMsg(c, "remoteOutboundSubscriptions", err)
		return
	}
	err := a.RemoteOutboundService.SetGroupConnections(payload.GroupId, payload.ConnectionIds, requestActor(c))
	jsonMsg(c, "update", err)
}

func (a *ApiService) ToggleRemoteOutboundGroupOutbounds(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	groupID, ok := remoteOutboundIDOrFail(c, "groupId")
	if !ok {
		return
	}
	result, err := a.RemoteOutboundService.ToggleGroupOutbounds(groupID, requestActor(c))
	jsonObj(c, result, err)
}

func (a *ApiService) MoveRemoteOutboundConnectionGroup(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	connectionID, ok := remoteOutboundIDOrFail(c, "connectionId")
	if !ok {
		return
	}
	groupID, ok := remoteOutboundIDOrFail(c, "groupId")
	if !ok {
		return
	}
	err := a.RemoteOutboundService.MoveConnectionToGroup(connectionID, groupID, requestActor(c))
	jsonMsg(c, "update", err)
}

func (a *ApiService) SyncRemoteOutboundConnection(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	connection, err := a.RemoteOutboundService.SyncConnectionToOutbound(id, requestActor(c))
	jsonObj(c, connection, err)
}

func (a *ApiService) TestRemoteOutboundConnection(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	result, err := a.RemoteOutboundService.CheckConnection(ctx, id, target)
	jsonObj(c, result, err)
}

func (a *ApiService) TestRemoteOutboundSubscription(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	id, ok := remoteOutboundIDOrFail(c, "id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	result, err := a.RemoteOutboundService.CheckSubscription(ctx, id, target)
	jsonObj(c, gin.H{"target": target, "results": result}, err)
}

func (a *ApiService) TestRemoteOutboundSubscriptions(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "remoteOutboundSubscriptions", "admin", "write") {
		return
	}
	target, ok := remoteOutboundCheckTarget(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	result, err := a.RemoteOutboundService.CheckAll(ctx, target)
	jsonObj(c, gin.H{"target": target, "results": result}, err)
}

func remoteOutboundCheckTarget(c *gin.Context) (string, bool) {
	target := strings.TrimSpace(c.DefaultQuery("target", remoteOutboundDefaultCheckTarget))
	if target == "" {
		target = remoteOutboundDefaultCheckTarget
	}
	if err := validateOutboundCheckTarget(c.Request.Context(), target); err != nil {
		jsonMsg(c, "remoteOutboundSubscriptions", err)
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
func remoteOutboundIDOrFail(c *gin.Context, name string) (uint, bool) {
	id, err := remoteOutboundID(c, name)
	if err != nil {
		jsonMsg(c, "remoteOutboundSubscriptions", err)
		return 0, false
	}
	return id, true
}
