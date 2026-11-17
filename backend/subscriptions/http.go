package subscriptions

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	subserver "github.com/MalenkiySolovey/solovey-ui/backend/subscriptions/server"

	"github.com/gin-gonic/gin"
)

type HTTPPublisher interface {
	GetSubs(string) (*string, []string, error)
	GetJSON(string) (*string, []string, error)
	GetClash(string) (*string, []string, error)
	GetXray(string) (*string, []string, error)
	LookupClient(string) (*model.Client, error)
}

type Handler struct {
	Settings  Settings
	Publisher HTTPPublisher
	Error     func(...any)
}

const maxSubscriptionHeaderBytes = 512

func (s *Handler) InitRouter(g *gin.RouterGroup, rateLimit gin.HandlerFunc) {
	g.Use(rateLimit)
	g.GET("/:subid", s.Subs)
	g.HEAD("/:subid", s.SubHeaders)
	g.GET("/json/:subid", s.Json)
	g.HEAD("/json/:subid", s.FormatHeaders("json"))
	g.GET("/clash/:subid", s.Clash)
	g.HEAD("/clash/:subid", s.FormatHeaders("clash"))
	g.GET("/xray/:subid", s.Xray)
	g.HEAD("/xray/:subid", s.FormatHeaders("xray"))
}

func (s *Handler) FormatHeaders(format string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("subscriptionFormat", format)
		s.SubHeaders(c)
	}
}

func (s *Handler) Subs(c *gin.Context) {
	format, isFormat := c.GetQuery("format")
	if isFormat {
		switch format {
		case "json":
			s.Json(c)
		case "clash":
			s.Clash(c)
		case "xray", "v2ray":
			s.Xray(c)
		default:
			c.String(400, "Error!")
		}
		return
	}
	if !s.SubLinkEnabled(c) {
		return
	}

	subID := c.Param("subid")
	result, headers, err := s.Publisher.GetSubs(subID)
	if err != nil || result == nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}

	s.WriteResult(c, result, headers)
}

func (s *Handler) Json(c *gin.Context) {
	if !s.SubFormatEnabled(c, "json") {
		return
	}
	result, headers, err := s.Publisher.GetJSON(c.Param("subid"))
	if err != nil || result == nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}
	s.WriteResult(c, result, headers)
}

func (s *Handler) Clash(c *gin.Context) {
	if !s.SubFormatEnabled(c, "clash") {
		return
	}
	result, headers, err := s.Publisher.GetClash(c.Param("subid"))
	if err != nil || result == nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}
	s.WriteResult(c, result, headers)
}

func (s *Handler) Xray(c *gin.Context) {
	if !s.SubFormatEnabled(c, "xray") {
		return
	}
	result, headers, err := s.Publisher.GetXray(c.Param("subid"))
	if err != nil || result == nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}
	s.WriteResult(c, result, headers)
}

func (s *Handler) SubHeaders(c *gin.Context) {
	format := c.Query("format")
	if taggedFormat, ok := c.Get("subscriptionFormat"); ok {
		format, _ = taggedFormat.(string)
	}
	if !s.SubFormatEnabled(c, format) {
		return
	}
	subID := c.Param("subid")
	client, err := s.Publisher.LookupClient(subID)
	if err != nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}

	displaySettings, err := subserver.CachedDisplaySettings(s.Settings, time.Now())
	if err != nil {
		s.logError(err)
		s.WriteError(c, err)
		return
	}
	headers := BuildClientHeaders(client, displaySettings)
	s.AddHeaders(c, headers)

	c.Status(200)
}

func (s *Handler) SubLinkEnabled(c *gin.Context) bool {
	return s.SubFormatEnabled(c, "")
}

func (s *Handler) SubFormatEnabled(c *gin.Context, format string) bool {
	var (
		enabled bool
		err     error
	)
	switch format {
	case "", "link":
		enabled, err = s.Settings.GetSubLinkEnable()
	case "json":
		enabled, err = s.Settings.GetSubJsonEnable()
	case "clash":
		enabled, err = s.Settings.GetSubClashEnable()
	case "xray", "v2ray":
		enabled, err = s.Settings.GetSubXrayEnable()
	default:
		c.String(400, "Error!")
		return false
	}
	if err != nil {
		s.logError(err)
		s.WriteError(c, err)
		return false
	}
	if !enabled {
		c.String(404, "Not Found")
		return false
	}
	return true
}

func (s *Handler) AddHeaders(c *gin.Context, headers []string) {
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

func (s *Handler) WriteResult(c *gin.Context, result *string, headers []string) {
	s.AddHeaders(c, headers)
	c.String(200, *result)
}

func (s *Handler) WriteError(c *gin.Context, err error) {
	if dbsqlite.IsNotFound(err) {
		subserver.NoteSubNotFound(c.ClientIP())
		c.String(404, "Not Found")
		return
	}
	c.String(400, "Error!")
}

func SafeSubscriptionHeaders(headers []string) []string {
	return subserver.SafeHeaders(headers, maxSubscriptionHeaderBytes)
}

func (s *Handler) logError(values ...any) {
	if s.Error != nil {
		s.Error(values...)
	}
}
