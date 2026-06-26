package netentity

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const clientHasInboundCondition = "? IN (SELECT json_each.value FROM json_each(clients.inbounds))"

func (s *InboundService) hasUser(inboundType string) bool {
	_, ok := userJSONField[inboundType]
	return ok
}

// userJSONField maps an inbound type to the JSON path used inside
// clients.config to locate per-user data. Do not extend this map without a
// positive list for both the inbound type and the JSON field value.
var userJSONField = map[string]string{
	"mixed":         "mixed",
	"socks":         "socks",
	"http":          "http",
	"shadowsocks":   "shadowsocks",
	"shadowsocks16": "shadowsocks",
	"vmess":         "vmess",
	"trojan":        "trojan",
	"naive":         "naive",
	"hysteria":      "hysteria",
	"shadowtls":     "shadowtls",
	"tuic":          "tuic",
	"hysteria2":     "hysteria2",
	"vless":         "vless",
	"anytls":        "anytls",
}

var allowedUserJSONFields = map[string]struct{}{
	"mixed":       {},
	"socks":       {},
	"http":        {},
	"shadowsocks": {},
	"vmess":       {},
	"trojan":      {},
	"naive":       {},
	"hysteria":    {},
	"shadowtls":   {},
	"tuic":        {},
	"hysteria2":   {},
	"vless":       {},
	"anytls":      {},
}

func (s *InboundService) AddUsers(db *gorm.DB, inboundJSON []byte, inboundID uint, inboundType string) ([]byte, error) {
	return s.addUsers(db, inboundJSON, inboundID, inboundType)
}

func (s *InboundService) FetchUsersByCondition(db *gorm.DB, inboundType, condition string, inbound map[string]interface{}, args ...interface{}) ([]json.RawMessage, error) {
	return s.fetchUsersByCondition(db, inboundType, condition, inbound, args...)
}

func (s *InboundService) addUsers(db *gorm.DB, inboundJson []byte, inboundId uint, inboundType string) ([]byte, error) {
	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	err := json.Unmarshal(inboundJson, &inbound)
	if err != nil {
		return nil, err
	}

	// A Trojan inbound authenticates per user; sing-box has no top-level
	// "password" field for it (only "users") and rejects the whole config
	// (`unknown field "password"`) if one is present. The inbound editor used to
	// write one for inbounds, so drop any leftover before emitting.
	if inboundType == "trojan" {
		delete(inbound, "password")
	}

	inbound["users"], err = s.fetchUsersByCondition(db, inboundType, clientHasInboundCondition, inbound, inboundId)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s *InboundService) fetchUsersByCondition(db *gorm.DB, inboundType string, condition string, inbound map[string]interface{}, args ...interface{}) ([]json.RawMessage, error) {
	if inboundType == "shadowtls" {
		version, _ := inbound["version"].(float64)
		if int(version) < 3 {
			return nil, nil
		}
	}
	if inboundType == "shadowsocks" {
		method, _ := inbound["method"].(string)
		if method == "2022-blake3-aes-128-gcm" {
			inboundType = "shadowsocks16"
		}
	}

	field, ok := userJSONField[inboundType]
	if !ok {
		return nil, common.NewErrorf("unsupported inbound type for user lookup: %s", inboundType)
	}
	if _, ok := allowedUserJSONFields[field]; !ok {
		return nil, common.NewErrorf("unsupported user JSON field for user lookup: %s", field)
	}

	var users []string
	// `field` is constrained to a static allow-list above, so embedding it
	// directly into the JSON path is safe. The dynamic condition is fed
	// through the query parameter slot to remain SQL-injection free.
	query := fmt.Sprintf(`SELECT json_extract(clients.config, '$.%s') FROM clients WHERE enable = true AND %s ORDER BY clients.sort_order, clients.id`, field, condition)
	err := db.Raw(query, args...).Scan(&users).Error
	if err != nil {
		return nil, err
	}
	// `xtls-rprx-vision` is strictly TCP. Xray-core rejects any vless
	// inbound that advertises the flow over a non-TCP transport (grpc,
	// ws, http, httpupgrade, ...) or without TLS. Strip the flow string
	// here so a single client UUID can be reused across multiple vless
	// inbounds with different transports without breaking the non-TCP
	// inbound (issue #1127).
	stripVisionFlow := false
	if inboundType == "vless" {
		if inbound["tls"] == nil {
			stripVisionFlow = true
		} else if transport, ok := inbound["transport"].(map[string]interface{}); ok {
			if tt, _ := transport["type"].(string); tt != "" && tt != "tcp" {
				stripVisionFlow = true
			}
		}
	}
	var usersJson []json.RawMessage
	for _, user := range users {
		if stripVisionFlow {
			user = strings.Replace(user, "xtls-rprx-vision", "", -1)
		}
		usersJson = append(usersJson, json.RawMessage(user))
	}
	return usersJson, nil
}
