package sub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type SubService struct {
	service.SettingService
}

func (s *SubService) GetSubs(subId string) (*string, []string, error) {
	now := time.Now()
	enabled, err := s.SettingService.GetSubLinkEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, common.NewError("raw link subscription disabled")
	}
	cacheKey := "base:" + subId
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	client, err := s.getClientBySubId(subId)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := subserver.CachedDisplaySettings(&s.SettingService, now)
	if err != nil {
		return nil, nil, err
	}

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
	return sublocal.ResolveClientLinks(rawLinks, mode, clientInfo)
}

func (j *SubService) getClientBySubId(subId string) (*model.Client, error) {
	db := dbsqlite.DB()
	client := &model.Client{}
	err := findUniqueEnabledClient(db, "sub_secret", subId, client)
	if err == nil {
		return client, entityclients.EnsureSubSecret(db, client)
	}
	if !dbsqlite.IsNotFound(err) {
		return nil, err
	}
	required, err := j.SettingService.GetSubSecretRequired()
	if err != nil {
		return nil, err
	}
	if required {
		return nil, gorm.ErrRecordNotFound
	}
	// Legacy name-based lookup, active only when the admin has disabled required
	// sub-secrets. Client names are admin-chosen and often guessable, so this
	// fallback allows unauthenticated enumeration of other clients' configs by
	// name. Warn whenever it actually serves a config so the operator is aware
	// the insecure mode is on (enable required sub-secrets to close it).
	err = findUniqueEnabledClient(db, "name", subId, client)
	if err != nil {
		return nil, err
	}
	logger.Warning("sub: served config via legacy name lookup (subSecretRequired is OFF) — enable required sub-secrets to prevent name-based enumeration")
	return client, entityclients.EnsureSubSecret(db, client)
}

func findUniqueEnabledClient(db *gorm.DB, column, value string, destination *model.Client) error {
	if db == nil || destination == nil {
		return errors.New("client lookup is unavailable")
	}
	if column != "sub_secret" && column != "name" {
		return errors.New("client lookup column is invalid")
	}
	var matches []model.Client
	if err := db.Model(model.Client{}).Where("enable = true AND "+column+" = ?", value).Limit(2).Find(&matches).Error; err != nil {
		return err
	}
	switch len(matches) {
	case 0:
		return gorm.ErrRecordNotFound
	case 1:
		*destination = matches[0]
		return nil
	default:
		return errors.New("ambiguous client subscription identity")
	}
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
