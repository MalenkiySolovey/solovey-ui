package entityinbounds

import (
	"encoding/json"
	"fmt"
	"strings"

	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type StoredUsers struct{}

const clientHasInboundCondition = "? IN (SELECT json_each.value FROM json_each(clients.inbounds))"

func (s StoredUsers) HasUser(inboundType string) bool {
	_, ok := clientfacts.UserField(inboundType)
	return ok
}

func (s StoredUsers) AddUsers(db *gorm.DB, inboundJSON []byte, inboundID uint, inboundType string) ([]byte, error) {
	return s.addUsers(db, inboundJSON, inboundID, inboundType)
}

func (s StoredUsers) addUsers(db *gorm.DB, inboundJson []byte, inboundId uint, inboundType string) ([]byte, error) {
	if !s.HasUser(inboundType) {
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

	inbound["users"], err = s.FetchUsers(db, inboundType, inbound, inboundId)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s StoredUsers) FetchUsers(db *gorm.DB, inboundType string, inbound map[string]interface{}, inboundID uint) ([]json.RawMessage, error) {
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

	field, ok := clientfacts.UserField(inboundType)
	if !ok {
		return nil, common.NewErrorf("unsupported inbound type for user lookup: %s", inboundType)
	}
	if !db.Migrator().HasTable("clients") {
		return nil, nil
	}

	var users []string
	// Both SQL fragments are static allow-listed values; inboundID remains a
	// bound parameter. Callers cannot inject an alternate WHERE condition.
	query := fmt.Sprintf(`SELECT json_extract(clients.config, '$.%s') FROM clients WHERE enable = true AND %s ORDER BY clients.sort_order, clients.id`, field, clientHasInboundCondition)
	err := db.Raw(query, inboundID).Scan(&users).Error
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

func (StoredUsers) ClientNamesByInboundIDs(db *gorm.DB, ids []uint) (map[uint][]string, error) {
	return entityclients.NamesByInboundIDs(db, ids)
}
