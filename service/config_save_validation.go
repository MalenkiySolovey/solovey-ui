package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// Native credentials, endpoints and inbound groups require a complete candidate: their
// Users, TLS and member owners supply fields that an isolated inbound draft
// cannot validate. Construct and close with the serving registry before the existing
// save transaction commits. Dry validation starts no listener or runtime.
func (s *ConfigService) validateNativeInboundCandidate(tx *gorm.DB) error {
	var kinds []string
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField != "" && entitycapabilities.Resolve("inbounds", fact.Type).Available {
			kinds = append(kinds, fact.Type)
		}
	}
	for _, kind := range []string{"bond", "core-failover", "call"} {
		if entitycapabilities.Resolve("inbounds", kind).Available {
			kinds = append(kinds, kind)
		}
	}
	var count int64
	if len(kinds) > 0 && tx.Migrator().HasTable(&model.Inbound{}) {
		if err := tx.Model(&model.Inbound{}).Where("type IN ?", kinds).Count(&count).Error; err != nil {
			return err
		}
	}
	var outboundCount int64
	var outboundKinds []string
	for _, kind := range []string{"call", "mieru", "trusttunnel", "sudoku", "masque", "bond", "core-failover"} {
		if entitycapabilities.Resolve("outbounds", kind).Available {
			outboundKinds = append(outboundKinds, kind)
		}
	}
	if len(outboundKinds) != 0 && tx.Migrator().HasTable(&model.Outbound{}) {
		if err := tx.Model(&model.Outbound{}).Where("type IN ?", outboundKinds).Count(&outboundCount).Error; err != nil {
			return err
		}
	}
	transportCandidate := false
	inboundRealmCandidate := false
	if registry.ResolveTransport("xhttp").Available() || registry.ResolveTransport("mkcp").Available() || registry.ResolveTransport("quic").Available() {
		for _, entity := range []any{&model.Inbound{}, &model.Outbound{}} {
			if !tx.Migrator().HasTable(entity) {
				continue
			}
			var rows []struct {
				Type    string
				Options json.RawMessage
			}
			if err := tx.Model(entity).Select("type", "options").Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				var options struct {
					Transport struct{ Type string }
					Realm     json.RawMessage
				}
				category := "outbounds"
				if _, inbound := entity.(*model.Inbound); inbound {
					category = "inbounds"
				}
				if row.Type == "hysteria2" && json.Unmarshal(row.Options, &options) == nil && len(options.Realm) != 0 && string(options.Realm) != "null" && entitycapabilities.ResolveResource(category, row.Type, row.Options).Available {
					transportCandidate = true
					inboundRealmCandidate = inboundRealmCandidate || category == "inbounds"
					break
				}
				if json.Unmarshal(row.Options, &options) == nil && (options.Transport.Type == "xhttp" || options.Transport.Type == "mkcp" || options.Transport.Type == "quic") && registry.ResolveTransport(options.Transport.Type).Available() {
					transportCandidate = true
					break
				}
			}
		}
	}
	var endpointCount int64
	var endpointKinds []string
	for _, kind := range []string{"openconnect", "openvpn-client"} {
		if entitycapabilities.Resolve("endpoints", kind).Available {
			endpointKinds = append(endpointKinds, kind)
		}
	}
	if len(endpointKinds) > 0 && tx.Migrator().HasTable(&model.Endpoint{}) {
		if err := tx.Model(&model.Endpoint{}).Where("type IN ?", endpointKinds).Count(&endpointCount).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&model.Endpoint{}) {
		var rows []model.Endpoint
		if err := tx.Select("type", "options", "ext").Where("type IN ?", []string{"wireguard", "warp"}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if !entitycapabilities.ResolveResource("endpoints", row.Type, row.Options).Available {
				continue
			}
			var metadata struct {
				Managed bool `json:"managed"`
			}
			if row.Type == "wireguard" && json.Unmarshal(row.Ext, &metadata) == nil && metadata.Managed {
				endpointCount++
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(row.Options, &fields) == nil {
				for key, raw := range fields {
					for field := range registry.CriticalProtocolFields()[row.Type+"/endpoint"] {
						if strings.EqualFold(key, field) && len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
							endpointCount++
							break
						}
					}
				}
			}
		}
	}
	var serviceCount int64
	if entitycapabilities.Resolve("services", "hysteria-realm").Available && tx.Migrator().HasTable(&model.Service{}) {
		if err := tx.Model(&model.Service{}).Where("type = ?", "hysteria-realm").Count(&serviceCount).Error; err != nil {
			return err
		}
	}
	if count == 0 && outboundCount == 0 && endpointCount == 0 && serviceCount == 0 && !transportCandidate {
		return nil
	}
	projection, err := s.singBoxConfigBuilder().BuildCandidateProjectionFromDB(tx, "", false)
	if err == nil {
		err = singboxvalidation.ValidateConfig(projection.Config)
	}
	if err != nil {
		// Provider diagnostics may quote configuration values. Return only an
		// owner code; the rejected candidate and its credential uses roll back.
		if serviceCount != 0 {
			return common.NewError("NATIVE_SERVICE_CANDIDATE_REJECTED: the complete provider configuration is invalid")
		}
		if endpointCount != 0 {
			return common.NewError("NATIVE_ENDPOINT_CANDIDATE_REJECTED: the complete provider configuration is invalid")
		}
		if count == 0 && !inboundRealmCandidate {
			return common.NewError("NATIVE_OUTBOUND_CANDIDATE_REJECTED: the complete provider configuration is invalid")
		}
		return common.NewError("NATIVE_INBOUND_CANDIDATE_REJECTED: the complete provider configuration is invalid")
	}
	return nil
}
