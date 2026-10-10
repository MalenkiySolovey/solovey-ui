package entityinbounds

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"gorm.io/gorm"
)

func StageStoredUpgrade(tx *gorm.DB) ([]diagnostics.Finding, error) {
	if !tx.Migrator().HasTable(&model.Inbound{}) {
		return nil, nil
	}
	var rows []model.Inbound
	if err := tx.Preload("Tls").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	findings := []diagnostics.Finding{}
	for _, row := range rows {
		path := fmt.Sprintf("inbounds[%d]", row.Id)
		source := row.Options
		if len(bytes.TrimSpace(source)) == 0 || bytes.Equal(bytes.TrimSpace(source), []byte("null")) {
			source = json.RawMessage(`{}`)
		}
		listen, err := PrepareOptionsUpgrade(row.Type, path, source)
		findings = append(findings, listen.Findings...)
		if err != nil {
			return findings, err
		}
		protocol, err := entityprotocol.PrepareUpgrade(row.Type, "inbound", path, listen.Candidate)
		findings = append(findings, protocol.Findings...)
		if err != nil {
			return findings, err
		}
		if !bytes.Equal(source, protocol.Candidate) {
			row.Options = protocol.Candidate
		}
		// Regenerate only the protocol-owned derived projection, preserving its
		// previously selected advertised address and unrelated operator fields.
		if row.Type == "hysteria" || row.Type == "hysteria2" || row.Type == "naive" {
			var outbound struct {
				Server string `json:"server"`
			}
			if len(bytes.TrimSpace(row.OutJson)) > 0 && !bytes.Equal(bytes.TrimSpace(row.OutJson), []byte("null")) {
				if err := json.Unmarshal(row.OutJson, &outbound); err != nil {
					return findings, err
				}
				if err := FillOutboundJSON(&row, outbound.Server); err != nil {
					return findings, err
				}
			}
		}
		projection, local, err := prepareOutboundTLSUpgrade(path+".out_json", row.OutJson)
		findings = append(findings, local...)
		if err != nil {
			return findings, err
		}
		row.OutJson = projection
		if err := tx.Model(&model.Inbound{}).Where("id = ?", row.Id).Updates(map[string]any{"options": row.Options, "out_json": row.OutJson}).Error; err != nil {
			return findings, err
		}
	}
	return findings, nil
}

func prepareOutboundTLSUpgrade(path string, source json.RawMessage) (json.RawMessage, []diagnostics.Finding, error) {
	if len(bytes.TrimSpace(source)) == 0 || bytes.Equal(bytes.TrimSpace(source), []byte("null")) {
		return source, nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return source, nil, fmt.Errorf("%s must be an object", path)
	}
	candidate, findings, err := entitytls.PrepareOptionsUpgrade(path+".tls", fields["tls"])
	if err != nil || len(findings) == 0 {
		return source, findings, err
	}
	fields["tls"] = candidate
	result, err := json.Marshal(fields)
	return result, findings, err
}
