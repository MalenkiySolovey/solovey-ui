package api

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) Save(c *gin.Context, loginUser string) {
	hostname := getHostname(c)
	obj := c.Request.FormValue("object")
	act := c.Request.FormValue("action")
	data := c.Request.FormValue("data")
	initUsers := c.Request.FormValue("initUsers")

	// Authoritative duplicate-create guard: an identical create resubmitted
	// within a short window (double-click / client double-send / proxy replay)
	// is skipped so it cannot insert a second row. Only creation actions on
	// entity objects are guarded; the claim is released below if the save fails
	// so a failed create can be retried immediately.
	dedupKey := ""
	if isDedupableSave(obj, act) {
		dedupKey = saveDedupKey(loginUser, obj, act, data)
		if !saveDedup.claim(dedupKey, time.Now().UnixNano()) {
			logger.Warning("save: skipped duplicate '", obj, "' create within dedup window")
			if err := a.LoadPartialData(c, []string{obj}); err != nil {
				jsonMsg(c, obj, err)
			}
			return
		}
	}

	subscriptionPathBefore := a.subscriptionPathSnapshot(obj, data)
	objs, err := a.ConfigService.Save(obj, act, json.RawMessage(data), initUsers, loginUser, hostname)
	if err != nil {
		if dedupKey != "" {
			saveDedup.release(dedupKey)
		}
		if a.handleSettingsSaveError(c, loginUser, obj, err) {
			return
		}
		jsonMsg(c, "save", err)
		return
	}
	// Save (incl. any synchronous core restart) succeeded and the row is
	// committed; keep deduplicating an identical resubmit for the window.
	if dedupKey != "" {
		saveDedup.complete(dedupKey, time.Now().UnixNano())
	}
	a.recordSettingsSaveSucceeded(c, loginUser, obj, act)
	a.auditSubscriptionPathChanges(c, loginUser, subscriptionPathBefore)
	err = a.LoadPartialData(c, objs)
	if err != nil {
		jsonMsg(c, obj, err)
	}
}
