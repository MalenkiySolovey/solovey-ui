//go:build !minimal

package importxui

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func secureClientConfig(email string) (map[string]map[string]any, error) {
	mixedPassword, err := common.SecureRandom(20)
	if err != nil {
		return nil, err
	}
	ssPassword16, err := secureBytesBase64(16)
	if err != nil {
		return nil, err
	}
	ssPassword32, err := secureBytesBase64(32)
	if err != nil {
		return nil, err
	}
	id, err := common.RandomUUID()
	if err != nil {
		return nil, err
	}
	return map[string]map[string]any{
		"mixed":       {"username": email, "password": mixedPassword},
		"socks":       {"username": email, "password": mixedPassword},
		"http":        {"username": email, "password": mixedPassword},
		"shadowsocks": {"name": email, "password": ssPassword32},
		"shadowsocks16": {
			"name": email, "password": ssPassword16,
		},
		"shadowtls": {"name": email, "password": ssPassword32},
		"vmess":     {"name": email, "uuid": id, "alterId": 0},
		"vless":     {"name": email, "uuid": id, "flow": "xtls-rprx-vision"},
		"anytls":    {"name": email, "password": mixedPassword},
		"trojan":    {"name": email, "password": mixedPassword},
		"naive":     {"username": email, "password": mixedPassword},
		"hysteria":  {"name": email, "auth_str": mixedPassword},
		"tuic":      {"name": email, "uuid": id, "password": mixedPassword},
		"hysteria2": {"name": email, "password": mixedPassword},
	}, nil
}

func secureBytesBase64(count int) (string, error) {
	value := make([]byte, count)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(value), nil
}
