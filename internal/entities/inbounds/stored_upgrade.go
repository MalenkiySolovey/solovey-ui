package entityinbounds

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
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
		if err := tx.Model(&model.Inbound{}).Where("id = ?", row.Id).Updates(map[string]any{"options": row.Options, "out_json": row.OutJson}).Error; err != nil {
			return findings, err
		}
	}
	return findings, nil
}
