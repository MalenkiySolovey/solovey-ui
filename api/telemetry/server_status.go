package telemetry

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetStats(c *gin.Context) {
	resource := c.Query("resource")
	tag := c.Query("tag")
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 100
	}
	data, err := a.StatsService.GetStats(resource, tag, limit)
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	a.JSONObj(c, data, err)
}

func (a *Handler) GetTrafficStats(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	buckets, _ := strconv.Atoi(c.Query("buckets"))
	endTime, _ := strconv.ParseInt(c.Query("end"), 10, 64)

	data, err := a.StatsService.GetInboundTrafficSummary(limit, buckets, endTime)
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	a.JSONObj(c, data, nil)
}

func (a *Handler) GetStatus(c *gin.Context) {
	request := c.Query("r")
	result := a.ServerService.GetStatus(request)
	a.JSONObj(c, result, nil)
}

func (a *Handler) GetOnlines(c *gin.Context) {
	onlines, err := a.StatsService.GetOnlines()
	a.JSONObj(c, onlines, err)
}

func (a *Handler) GetLogs(c *gin.Context) {
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
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "logs: " + err.Error()})
		return
	}
	a.JSONObj(c, logs, nil)
}

func (a *Handler) GetLogEntries(c *gin.Context) {
	if !a.RequireScope(c, "logs", "admin", "read", "write", "observability") {
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
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "logs: " + err.Error()})
		return
	}
	a.JSONObj(c, logs, nil)
}

func (a *Handler) GetKeypairs(c *gin.Context) {
	kType := c.Query("k")
	options := c.Query("o")
	keypair := a.ServerService.GenKeypair(kType, options)
	a.JSONObj(c, keypair, nil)
}
