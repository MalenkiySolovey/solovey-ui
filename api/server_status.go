package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) GetStats(c *gin.Context) {
	resource := c.Query("resource")
	tag := c.Query("tag")
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 100
	}
	data, err := a.StatsService.GetStats(resource, tag, limit)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, err)
}

func (a *ApiService) GetStatus(c *gin.Context) {
	request := c.Query("r")
	result := a.ServerService.GetStatus(request)
	jsonObj(c, result, nil)
}

func (a *ApiService) GetOnlines(c *gin.Context) {
	onlines, err := a.StatsService.GetOnlines()
	jsonObj(c, onlines, err)
}

func (a *ApiService) GetLogs(c *gin.Context) {
	count := c.Query("count")
	if count == "" {
		count = c.Query("c")
	}
	level := c.Query("level")
	if level == "" {
		level = c.Query("l")
	}
	logs, err := a.ServerService.GetLogsFiltered(count, level, c.Query("source"), c.Query("filter"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "logs: " + err.Error()})
		return
	}
	jsonObj(c, logs, nil)
}

func (a *ApiService) GetLogEntries(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "logs", "admin", "read", "write", "observability") {
		return
	}
	count := c.Query("count")
	if count == "" {
		count = c.Query("c")
	}
	level := c.Query("level")
	if level == "" {
		level = c.Query("l")
	}
	logs, err := a.ServerService.GetLogEntriesFiltered(count, level, c.Query("source"), c.Query("filter"), c.Query("category"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "logs: " + err.Error()})
		return
	}
	jsonObj(c, logs, nil)
}

func (a *ApiService) GetKeypairs(c *gin.Context) {
	kType := c.Query("k")
	options := c.Query("o")
	keypair := a.ServerService.GenKeypair(kType, options)
	jsonObj(c, keypair, nil)
}
