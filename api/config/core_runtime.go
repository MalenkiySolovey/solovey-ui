package config

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"

	"github.com/gin-gonic/gin"
)

func (a *Handler) RestartApp(c *gin.Context) {
	scheduler := a.RestartScheduler
	if scheduler == nil {
		runtime := a.Runtime
		if runtime == nil {
			runtime = service.DefaultRuntime()
		}
		scheduler = runtime.RestartScheduler()
	}
	err := scheduler.ScheduleRestart(3 * time.Second)
	a.JSONMsg(c, "restartApp", err)
}

func (a *Handler) RestartSb(c *gin.Context) {
	err := a.ConfigService.RestartCore()
	if err != nil {
		a.TelegramService.NotifyTelegramEvent("core_restart_failed", a.coreRestartFailureFields(c, err))
	} else {
		a.TelegramService.NotifyTelegramEvent("core_restarted", nil)
	}
	a.JSONMsg(c, "restartSb", err)
}

func (a *Handler) GetSingboxConfig(c *gin.Context) {
	rawConfig, err := a.ConfigService.GetConfig("")
	if err != nil {
		c.Status(400)
		_, _ = c.Writer.WriteString(err.Error())
		return
	}
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=config_"+time.Now().Format("20060102-150405")+".json")
	_, _ = c.Writer.Write(*rawConfig)
}

func (a *Handler) GetCheckOutbound(c *gin.Context) {
	tag := c.Query("tag")
	link := c.Query("link")
	// A user-supplied custom test target is an SSRF vector: the server fetches it
	// through the selected outbound. Validate it exactly like the plural
	// CheckOutbounds endpoint. An empty link means "use the core's built-in
	// default target" (the normal UI call passes only tag) and is safe.
	if link != "" {
		if err := a.ValidateTarget(c.Request.Context(), link); err != nil {
			a.JSONMsg(c, "checkOutbound", err)
			return
		}
	}
	result := a.ConfigService.CheckOutbound(tag, link)
	a.JSONObj(c, result, nil)
}

func (a *Handler) CheckOutbounds(c *gin.Context) {
	target := c.DefaultPostForm("target", "https://www.gstatic.com/generate_204")
	if err := a.ValidateTarget(c.Request.Context(), target); err != nil {
		a.JSONMsg(c, "checkOutbounds", err)
		return
	}
	outbounds, err := a.OutboundService.GetAll()
	if err != nil {
		a.JSONMsg(c, "checkOutbounds", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	type checkResult struct {
		Tag     string `json:"tag"`
		OK      bool   `json:"ok"`
		Delay   uint16 `json:"delay"`
		Error   string `json:"error,omitempty"`
		Skipped bool   `json:"skipped,omitempty"`
	}
	results := make([]checkResult, len(*outbounds))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, outbound := range *outbounds {
		tag, _ := outbound["tag"].(string)
		if tag == "" {
			results[i] = checkResult{Skipped: true, Error: "missing tag"}
			continue
		}
		results[i].Tag = tag
		wg.Add(1)
		go func(index int, outboundTag string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index].Error = ctx.Err().Error()
				return
			}
			checkCtx, cancelCheck := context.WithTimeout(ctx, 5*time.Second)
			defer cancelCheck()
			check := a.ConfigService.CheckOutboundWithContext(checkCtx, outboundTag, target)
			results[index].OK = check.OK
			results[index].Delay = check.Delay
			results[index].Error = check.Error
		}(i, tag)
	}
	wg.Wait()
	a.JSONObj(c, gin.H{
		"target":  target,
		"results": results,
	}, nil)
}

func ValidateOutboundCheckTarget(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return common.NewError("check target must be an HTTPS URL")
	}
	if parsed.User != nil {
		return common.NewError("check target must not include userinfo")
	}
	return ssrf.ValidateOutboundURL(ctx, rawURL, "https")
}
