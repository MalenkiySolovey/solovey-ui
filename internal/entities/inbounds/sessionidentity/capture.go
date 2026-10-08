// Package sessionidentity captures client bindings from the same desired-state
// view used to render an inbound. Credentials are compared locally and never
// carried into runtime identity, responses, logs or persistence.
package sessionidentity

import (
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"gorm.io/gorm"
)

type candidate struct {
	Type  string           `json:"type"`
	Tag   string           `json:"tag"`
	Users []map[string]any `json:"users"`
}

// Capture accepts a complete rendered config or one owner-rendered inbound.
// It never associates a client using its display name, IP or a caller's ID.
func Capture(db *gorm.DB, config []byte) ([]inboundidentity.Binding, error) {
	var root struct {
		Inbounds []candidate `json:"inbounds"`
		candidate
	}
	if err := json.Unmarshal(config, &root); err != nil {
		return nil, errors.New("runtime identity config is invalid")
	}
	rows := root.Inbounds
	if root.Tag != "" {
		rows = []candidate{root.candidate}
	}
	hasUsers := false
	for _, row := range rows {
		hasUsers = hasUsers || len(row.Users) > 0
	}
	if !hasUsers {
		return nil, nil
	}
	if db == nil {
		return nil, errors.New("runtime identity catalogue unavailable")
	}
	if !db.Migrator().HasTable(&model.Client{}) {
		return nil, nil
	}
	var inbounds []model.Inbound
	if err := db.Select("id", "tag", "type").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	byTag := map[string]model.Inbound{}
	for _, row := range inbounds {
		byTag[row.Tag] = row
	}
	var clients []model.Client
	if err := db.Select("id", "config", "inbounds").Where("enable = ?", true).Find(&clients).Error; err != nil {
		return nil, err
	}
	prepared := prepareClients(clients)
	result := []inboundidentity.Binding{}
	for _, row := range rows {
		stored, ok := byTag[row.Tag]
		if !ok || stored.Type != row.Type {
			continue
		}
		field, supported := clientfacts.UserField(row.Type)
		if !supported {
			continue
		}
		byPrincipal := map[string]uint{}
		ambiguous := map[string]bool{}
		seen := map[string]bool{}
		expectedByPrincipal := map[string][]desiredUser{}
		for _, client := range prepared {
			if !client.inbounds[stored.Id] {
				continue
			}
			expected := client.credentials[field]
			name := principal(expected, row.Type)
			if name != "" {
				expectedByPrincipal[name] = append(expectedByPrincipal[name], desiredUser{id: client.id, credentials: expected})
			}
		}
		for _, actual := range row.Users {
			name := principal(actual, row.Type)
			if name == "" {
				continue
			}
			if seen[name] {
				ambiguous[name] = true
			}
			seen[name] = true
			for _, client := range expectedByPrincipal[name] {
				if !equivalent(actual, client.credentials, row.Type) {
					continue
				}
				if prior := byPrincipal[name]; prior != 0 && prior != client.id {
					ambiguous[name] = true
				}
				byPrincipal[name] = client.id
			}
		}
		names := make([]string, 0, len(byPrincipal))
		for name := range byPrincipal {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			id := byPrincipal[name]
			if id != 0 && !ambiguous[name] {
				result = append(result, inboundidentity.Binding{Inbound: row.Tag, Principal: name, ClientID: id})
			}
		}
	}
	return result, nil
}

func principal(user map[string]any, kind string) string {
	field := "name"
	if kind == "http" || kind == "socks" || kind == "mixed" || kind == "naive" {
		field = "username"
	}
	value, _ := user[field].(string)
	return value
}

type desiredClient struct {
	id          uint
	inbounds    map[uint]bool
	credentials map[string]map[string]any
}
type desiredUser struct {
	id          uint
	credentials map[string]any
}

func prepareClients(clients []model.Client) []desiredClient {
	result := make([]desiredClient, 0, len(clients))
	for _, client := range clients {
		prepared := desiredClient{id: client.Id, inbounds: map[uint]bool{}}
		var ids []uint
		if json.Unmarshal(client.Inbounds, &ids) != nil || json.Unmarshal(client.Config, &prepared.credentials) != nil {
			continue
		}
		for _, id := range ids {
			prepared.inbounds[id] = true
		}
		result = append(result, prepared)
	}
	return result
}

func equivalent(actual, expected map[string]any, kind string) bool {
	if len(expected) == 0 {
		return false
	}
	if kind == "vless" {
		// The existing inbound renderer strips Vision on incompatible delivery.
		// Flow is not an authentication credential or a client identity field.
		actual, expected = maps.Clone(actual), maps.Clone(expected)
		delete(actual, "flow")
		delete(expected, "flow")
	}
	return reflect.DeepEqual(actual, expected)
}
