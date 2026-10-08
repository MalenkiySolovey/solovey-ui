package outbounds

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
	if !tx.Migrator().HasTable(&model.Outbound{}) {
		return nil, nil
	}
	var rows []model.Outbound
	if err := tx.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	findings := []diagnostics.Finding{}
	for _, row := range rows {
		path := fmt.Sprintf("outbounds[%d]", row.Id)
		source := row.Options
		if len(bytes.TrimSpace(source)) == 0 || bytes.Equal(bytes.TrimSpace(source), []byte("null")) {
			source = json.RawMessage(`{}`)
		}
		candidate, local, err := PrepareOptionsUpgrade(row.Type, path, source)
		findings = append(findings, local...)
		if err != nil {
			return findings, err
		}
		protocol, err := entityprotocol.PrepareUpgrade(row.Type, "outbound", path, candidate)
		findings = append(findings, protocol.Findings...)
		if err != nil {
			return findings, err
		}
		candidate = protocol.Candidate
		var fields map[string]json.RawMessage
		if json.Unmarshal(candidate, &fields) == nil {
			if raw, present := fields["tls"]; present {
				prepared, local, err := entitytls.PrepareOptionsUpgrade(path+".tls", raw)
				findings = append(findings, local...)
				if err != nil {
					return findings, err
				}
				if !bytes.Equal(raw, prepared) {
					fields["tls"] = prepared
					candidate, _ = json.Marshal(fields)
				}
			}
		}
		if !bytes.Equal(source, candidate) {
			if err := tx.Model(&model.Outbound{}).Where("id = ?", row.Id).Update("options", candidate).Error; err != nil {
				return findings, err
			}
		}
	}
	return findings, nil
}
