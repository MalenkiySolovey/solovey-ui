package sub

import (
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	SubService
	JSONService
	ClashService
	XrayService
}

const maxSubscriptionHeaderBytes = 512

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.Use(subserver.RateLimitMiddleware())
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
	g.GET("/json/:subid", s.json)
	g.HEAD("/json/:subid", s.formatHeaders("json"))
	g.GET("/clash/:subid", s.clash)
	g.HEAD("/clash/:subid", s.formatHeaders("clash"))
	g.GET("/xray/:subid", s.xray)
	g.HEAD("/xray/:subid", s.formatHeaders("xray"))
}

func (s *SubHandler) formatHeaders(format string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("subscriptionFormat", format)
		s.subHeaders(c)
	}
}

func (s *SubHandler) subs(c *gin.Context) {
	format, isFormat := c.GetQuery("format")
	if isFormat {
		switch format {
		case "json":
			s.json(c)
		case "clash":
			s.clash(c)
		case "xray", "v2ray":
			s.xray(c)
		default:
			c.String(400, "Error!")
		}
		return
	}
	if !s.subLinkEnabled(c) {
		return
	}

	subID := c.Param("subid")
	result, headers, err := s.SubService.GetSubs(subID)
	if err != nil || result == nil {
		logger.Error(err)
		s.writeError(c, err)
		return
	}

	s.writeResult(c, result, headers)
}

func (s *SubHandler) json(c *gin.Context) {
	if !s.subFormatEnabled(c, "json") {
		return
	}
	result, headers, err := s.JSONService.GetJSON(c.Param("subid"))
	if err != nil || result == nil {
		logger.Error(err)
		s.writeError(c, err)
		return
	}
	s.writeResult(c, result, headers)
}

func (s *SubHandler) clash(c *gin.Context) {
	if !s.subFormatEnabled(c, "clash") {
		return
	}
	result, headers, err := s.ClashService.GetClash(c.Param("subid"))
	if err != nil || result == nil {
		logger.Error(err)
		s.writeError(c, err)
		return
	}
	s.writeResult(c, result, headers)
}

func (s *SubHandler) xray(c *gin.Context) {
	if !s.subFormatEnabled(c, "xray") {
		return
	}
	result, headers, err := s.XrayService.GetXray(c.Param("subid"))
	if err != nil || result == nil {
		logger.Error(err)
		s.writeError(c, err)
		return
	}
	s.writeResult(c, result, headers)
}

func (s *SubHandler) subHeaders(c *gin.Context) {
	format := c.Query("format")
	if taggedFormat, ok := c.Get("subscriptionFormat"); ok {
		format, _ = taggedFormat.(string)
	}
	if !s.subFormatEnabled(c, format) {
		return
	}
	subID := c.Param("subid")
	client, err := s.SubService.getClientBySubId(subID)
	if err != nil {
		logger.Error(err)
		s.writeError(c, err)
		return
	}

	headers := buildClientHeaders(client, subserver.CachedDisplaySettings(&s.SettingService, time.Now()))
	s.addHeaders(c, headers)

	c.Status(200)
}

func (s *SubHandler) subLinkEnabled(c *gin.Context) bool {
	return s.subFormatEnabled(c, "")
}

func (s *SubHandler) subFormatEnabled(c *gin.Context, format string) bool {
	var (
		enabled bool
		err     error
	)
	switch format {
	case "", "link":
		enabled, err = s.SettingService.GetSubLinkEnable()
	case "json":
		enabled, err = s.SettingService.GetSubJsonEnable()
	case "clash":
		enabled, err = s.SettingService.GetSubClashEnable()
	case "xray", "v2ray":
		enabled, err = s.SettingService.GetSubXrayEnable()
	default:
		c.String(400, "Error!")
		return false
	}
	if err != nil {
		logger.Error(err)
		s.writeError(c, err)
		return false
	}
	if !enabled {
		c.String(404, "Not Found")
		return false
	}
	return true
}

func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
	if len(headers) < 3 {
		return
	}
	headers = safeSubscriptionHeaders(headers)
	c.Writer.Header().Set("Subscription-Userinfo", headers[0])
	c.Writer.Header().Set("Profile-Update-Interval", headers[1])
	c.Writer.Header().Set("Profile-Title", headers[2])
	if len(headers) > 3 && headers[3] != "" {
		c.Writer.Header().Set("Support-Url", headers[3])
	}
	if len(headers) > 4 && headers[4] != "" {
		c.Writer.Header().Set("Profile-Web-Page-Url", headers[4])
	}
	if len(headers) > 5 && headers[5] != "" {
		c.Writer.Header().Set("Profile-Announcement", headers[5])
	}
}

func (s *SubHandler) writeResult(c *gin.Context, result *string, headers []string) {
	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) writeError(c *gin.Context, err error) {
	if dbsqlite.IsNotFound(err) {
		subserver.NoteSubNotFound(c.ClientIP())
		c.String(404, "Not Found")
		return
	}
	c.String(400, "Error!")
}

func safeSubscriptionHeaders(headers []string) []string {
	return subserver.SafeHeaders(headers, maxSubscriptionHeaderBytes)
}
