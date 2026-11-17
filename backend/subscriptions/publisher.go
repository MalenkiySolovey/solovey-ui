// Package subscriptions owns subscription admission, output caching and HTTP publishing.
package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	subserver "github.com/MalenkiySolovey/solovey-ui/backend/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/backend/users"
	"gorm.io/gorm"
	"strings"
	"time"
)

// Settings exposes the current security gates and presentation snapshot.
type Settings interface {
	subserver.DisplaySettingsReader
	GetSubSecretRequired() (bool, error)
	GetSubLinkEnable() (bool, error)
	GetSubJsonEnable() (bool, error)
	GetSubClashEnable() (bool, error)
	GetSubXrayEnable() (bool, error)
}

// Projection receives an authorized User and retains provider-native rendering.
type Projection interface {
	PrivateLinks(*model.Client) (*model.Client, error)
	LoadDelivery(*model.Client) (*model.Client, []*model.Inbound, error)
	ResolveLinks(json.RawMessage, string) []string
	RenderJSON(*model.Client, []*model.Inbound) (string, error)
	RenderClash(*model.Client, []*model.Inbound) (string, error)
	RenderXray(*model.Client, []*model.Inbound) (string, error)
	ContributorsVersion() uint64
}
type Publisher struct {
	Settings   Settings
	Projection Projection
	Warning    func(...any)
}

func (p Publisher) validate() error {
	if p.Settings == nil {
		return errors.New("subscription settings owner is unavailable")
	}
	if p.Projection == nil {
		return errors.New("subscription projection owner is unavailable")
	}
	return nil
}
func (p Publisher) warning(values ...any) {
	if p.Warning != nil {
		p.Warning(values...)
	}
}
func (p Publisher) LoadClientData(id string) (*model.Client, []*model.Inbound, error) {
	if err := p.validate(); err != nil {
		return nil, nil, err
	}
	c, err := p.LookupClient(id)
	if err != nil {
		return nil, nil, err
	}
	return p.Projection.LoadDelivery(c)
}
func (p Publisher) GetSubs(subId string) (*string, []string, error) {
	if err := p.validate(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	enabled, err := p.Settings.GetSubLinkEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, errors.New("raw link subscription disabled")
	}
	cacheKey := "base:" + subId
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	client, err := p.LookupClient(subId)
	if err != nil {
		return nil, nil, err
	}
	client, err = p.Projection.PrivateLinks(client)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := subserver.CachedDisplaySettings(p.Settings, now)
	if err != nil {
		return nil, nil, err
	}

	clientInfo := ""
	if cfg.ShowInfo {
		clientInfo = p.getClientInfo(client)
	}
	if cfg.NameInRemark {
		clientInfo = " " + client.Name + clientInfo
	}

	linksArray := p.Projection.ResolveLinks(client.Links, clientInfo)
	result := strings.Join(linksArray, "\n")

	headers := BuildClientHeaders(client, cfg)

	if cfg.Encode {
		result = base64.StdEncoding.EncodeToString([]byte(result))
	}

	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}

func (p Publisher) LookupClient(subId string) (*model.Client, error) {
	if p.Settings == nil {
		return nil, errors.New("subscription settings owner is unavailable")
	}
	db := dbsqlite.DB()
	client := &model.Client{}
	err := findUniqueEnabledClient(db, "sub_secret", subId, client)
	if err == nil {
		return client, users.EnsureSubSecret(db, client)
	}
	if !dbsqlite.IsNotFound(err) {
		return nil, err
	}
	required, err := p.Settings.GetSubSecretRequired()
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
	p.warning("sub: served config via legacy name lookup (subSecretRequired is OFF) — enable required sub-secrets to prevent name-based enumeration")
	return client, users.EnsureSubSecret(db, client)
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

func BuildClientHeaders(client *model.Client, cfg subserver.DisplaySettings) []string {
	headers := []string{fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", client.Up, client.Down, client.Volume, client.Expiry), fmt.Sprintf("%d", cfg.Updates), client.Name}
	if cfg.Title != "" {
		headers[2] = cfg.Title
	}
	headers = append(headers, cfg.SupportURL, cfg.ProfileURL, cfg.Announce)
	return headers
}

func (p Publisher) getClientInfo(c *model.Client) string {
	now := time.Now().Unix()

	var result []string
	if vol := c.Volume - (c.Up + c.Down); vol > 0 {
		result = append(result, fmt.Sprintf("%s%s", p.formatTraffic(vol), "📊"))
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

func (p Publisher) formatTraffic(trafficBytes int64) string {
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
