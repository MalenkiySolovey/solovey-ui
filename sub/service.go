package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gofrs/uuid/v5"
	"gorm.io/gorm"
)

type SubService struct {
	service.SettingService
}

func (s *SubService) GetSubs(subId string) (*string, []string, error) {
	now := time.Now()
	cacheKey := "base:" + subId
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	client, err := s.getClientBySubId(subId)
	if err != nil {
		return nil, nil, err
	}

	cfg := subserver.CachedDisplaySettings(&s.SettingService, now)

	clientInfo := ""
	if cfg.ShowInfo {
		clientInfo = s.getClientInfo(client)
	}
	if cfg.NameInRemark {
		clientInfo = " " + client.Name + clientInfo
	}

	linksArray := resolveClientLinks(client.Links, sublocal.LinkModeAll, clientInfo)
	result := strings.Join(linksArray, "\n")

	headers := buildClientHeaders(client, cfg)

	if cfg.Encode {
		result = base64.StdEncoding.EncodeToString([]byte(result))
	}

	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}

func resolveClientLinks(rawLinks json.RawMessage, mode sublocal.LinkMode, clientInfo string) []string {
	enabled, err := (&service.SettingService{}).GetSubLinkEnable()
	if err == nil && !enabled {
		return nil
	}
	return sublocal.ResolveClientLinks(rawLinks, mode, clientInfo)
}

func (j *SubService) getClientBySubId(subId string) (*model.Client, error) {
	db := dbsqlite.DB()
	client := &model.Client{}
	err := db.Model(model.Client{}).Where("enable = true and sub_secret = ?", subId).First(client).Error
	if err == nil {
		return client, j.ensureClientSubSecret(db, client)
	}
	if !dbsqlite.IsNotFound(err) {
		return nil, err
	}
	required, _ := j.SettingService.GetSubSecretRequired()
	if required {
		return nil, gorm.ErrRecordNotFound
	}
	// Legacy name-based lookup, active only when the admin has disabled required
	// sub-secrets. Client names are admin-chosen and often guessable, so this
	// fallback allows unauthenticated enumeration of other clients' configs by
	// name. Warn whenever it actually serves a config so the operator is aware
	// the insecure mode is on (enable required sub-secrets to close it).
	err = db.Model(model.Client{}).Where("enable = true and name = ?", subId).First(client).Error
	if err != nil {
		return nil, err
	}
	logger.Warning("sub: served config via legacy name lookup (subSecretRequired is OFF) — enable required sub-secrets to prevent name-based enumeration")
	return client, j.ensureClientSubSecret(db, client)
}

func loadClientData(subID string) (*model.Client, []*model.Inbound, error) {
	db := dbsqlite.DB()
	client, err := (&SubService{}).getClientBySubId(subID)
	if err != nil {
		return nil, nil, err
	}
	var inboundIDs []uint
	if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
		return nil, nil, err
	}
	var inbounds []*model.Inbound
	if err := db.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIDs).Find(&inbounds).Error; err != nil {
		return nil, nil, err
	}
	return client, inbounds, nil
}

func buildClientHeaders(client *model.Client, cfg subserver.DisplaySettings) []string {
	headers := sublocal.ClientHeaders(client, cfg.Updates)
	if cfg.Title != "" {
		headers[2] = cfg.Title
	}
	headers = append(headers, cfg.SupportURL, cfg.ProfileURL, cfg.Announce)
	return headers
}

func (s *SubService) getClientInfo(c *model.Client) string {
	now := time.Now().Unix()

	var result []string
	if vol := c.Volume - (c.Up + c.Down); vol > 0 {
		result = append(result, fmt.Sprintf("%s%s", s.formatTraffic(vol), "📊"))
	}
	if c.Expiry > 0 {
		result = append(result, fmt.Sprintf("%d%s⏳", (c.Expiry-now)/86400, "Days"))
	}
	if len(result) > 0 {
		return " " + strings.Join(result, " ")
	} else {
		return " ♾"
	}
}

func (s *SubService) formatTraffic(trafficBytes int64) string {
	if trafficBytes < 1024 {
		return fmt.Sprintf("%.2fB", float64(trafficBytes)/float64(1))
	} else if trafficBytes < (1024 * 1024) {
		return fmt.Sprintf("%.2fKB", float64(trafficBytes)/float64(1024))
	} else if trafficBytes < (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fMB", float64(trafficBytes)/float64(1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fGB", float64(trafficBytes)/float64(1024*1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fTB", float64(trafficBytes)/float64(1024*1024*1024*1024))
	} else {
		return fmt.Sprintf("%.2fEB", float64(trafficBytes)/float64(1024*1024*1024*1024*1024))
	}
}

func (s *SubService) ensureClientSubSecret(db *gorm.DB, client *model.Client) error {
	if client.SubSecret != "" {
		return nil
	}
	secret, err := uuid.NewV4()
	if err != nil {
		return err
	}
	client.SubSecret = secret.String()
	return db.Model(model.Client{}).Where("id = ?", client.Id).Update("sub_secret", client.SubSecret).Error
}
