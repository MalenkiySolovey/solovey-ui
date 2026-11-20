//go:build !minimal

package telegram

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/customerbot"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gofrs/uuid/v5"
	"gorm.io/gorm"
)

// createTrialClient executes the existing Panel client command on behalf of a
// Paid trial proposal. The customer bot owns eligibility and binding state.
func (b *Bot) createTrialClient(ctx context.Context, db *gorm.DB, trial customerbot.TrialClient) (uint, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if b.runtime == nil {
		return 0, fmt.Errorf("auto-register runtime unavailable")
	}
	name := b.uniqueClientName(db, trial.TelegramID)
	config, err := generateClientConfig(name)
	if err != nil {
		return 0, fmt.Errorf("generate client config: %w", err)
	}
	host, _ := b.setting.GetWebDomain()
	cfg := service.NewConfigServiceWithRuntime(b.runtime)
	return cfg.ProvisionClient(ctx, service.ClientProvision{
		Name: name, Config: config, Inbounds: trial.Inbounds,
		Volume: trial.Volume, Expiry: trial.Expiry, Description: trial.Description,
	}, "PaidSubBot", host)
}

// uniqueClientName derives a collision-free name from the Telegram id.
func (b *Bot) uniqueClientName(db *gorm.DB, tgID int64) string {
	base := "tg" + strconv.FormatInt(tgID, 10)
	name := base
	for i := 0; i < 50; i++ {
		var cnt int64
		db.Model(&model.Client{}).Where("name = ?", name).Count(&cnt)
		if cnt == 0 {
			return name
		}
		name = base + "_" + common.RandomSuffix(4)
	}
	return base + "_" + common.RandomSuffix(8)
}

// generateClientConfig builds a full per-protocol client config, mirroring the
// panel's randomConfigs (frontend/src/types/clients.ts) so the resulting client
// works on any assigned inbound type. Both shadowsocks key lengths are emitted
// (32-byte for legacy/2022-256, 16-byte for 2022-blake3-aes-128-gcm).
func generateClientConfig(name string) (json.RawMessage, error) {
	mixedPassword, err := common.SecureRandom(10)
	if err != nil {
		return nil, err
	}
	ss32, err := randomSSPassword(32)
	if err != nil {
		return nil, err
	}
	ss16, err := randomSSPassword(16)
	if err != nil {
		return nil, err
	}
	u, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	uuidStr := u.String()
	cfg := map[string]map[string]any{
		"mixed":         {"username": name, "password": mixedPassword},
		"socks":         {"username": name, "password": mixedPassword},
		"http":          {"username": name, "password": mixedPassword},
		"shadowsocks":   {"name": name, "password": ss32},
		"shadowsocks16": {"name": name, "password": ss16},
		"shadowtls":     {"name": name, "password": ss32},
		"vmess":         {"name": name, "uuid": uuidStr, "alterId": 0},
		"vless":         {"name": name, "uuid": uuidStr, "flow": "xtls-rprx-vision"},
		"anytls":        {"name": name, "password": mixedPassword},
		"trojan":        {"name": name, "password": mixedPassword},
		"naive":         {"username": name, "password": mixedPassword},
		"hysteria":      {"name": name, "auth_str": mixedPassword},
		"tuic":          {"name": name, "uuid": uuidStr, "password": mixedPassword},
		"hysteria2":     {"name": name, "password": mixedPassword},
	}
	return json.Marshal(cfg)
}

func randomSSPassword(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}
