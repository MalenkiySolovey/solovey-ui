package entityclients

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"gorm.io/gorm"
)

// MergeImportedMembership attaches an existing VPN user to mapped local
// inbounds. This intent cannot alter access, credentials, tokens or accounting;
// the general editor Save command deliberately has broader enable semantics.
func MergeImportedMembership(tx *gorm.DB, id uint, added []uint, hostname string) error {
	if tx == nil || id == 0 {
		return errors.New("client membership target is unavailable")
	}
	var client model.Client
	if err := tx.First(&client, id).Error; err != nil {
		return err
	}
	var current []uint
	if err := json.Unmarshal(client.Inbounds, &current); err != nil {
		return err
	}
	for _, inboundID := range added {
		if inboundID == 0 {
			return errors.New("client membership requires a local inbound identity")
		}
		var inbound model.Inbound
		if err := tx.First(&inbound, inboundID).Error; err != nil {
			return err
		}
	}
	current = append(current, added...)
	return persistImportedMembership(tx, &client, current, hostname)
}

// RebindImportedInbound preserves memberships when an explicitly approved
// replacement recreates an inbound row. Only membership and derived links move.
func RebindImportedInbound(tx *gorm.DB, oldID, newID uint, hostname string) error {
	if tx == nil || oldID == 0 || newID == 0 || oldID == newID {
		return errors.New("invalid inbound replacement identity")
	}
	var replacement model.Inbound
	if err := tx.First(&replacement, newID).Error; err != nil {
		return err
	}
	clients, err := ByInbound(tx, oldID)
	if err != nil {
		return err
	}
	for _, client := range clients {
		var memberships []uint
		if err := json.Unmarshal(client.Inbounds, &memberships); err != nil {
			return err
		}
		for index, id := range memberships {
			if id == oldID {
				memberships[index] = newID
			}
		}
		if err := persistImportedMembership(tx, &client, memberships, hostname); err != nil {
			return err
		}
	}
	return nil
}

func persistImportedMembership(tx *gorm.DB, client *model.Client, memberships []uint, hostname string) error {
	if err := jsonvalue.OptionalObject("client config", client.Config); err != nil {
		return err
	}
	if err := jsonvalue.OptionalArray("client links", client.Links); err != nil {
		return err
	}
	seen := make(map[uint]bool, len(memberships))
	unique := make([]uint, 0, len(memberships))
	for _, id := range memberships {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	raw, err := json.Marshal(unique)
	if err != nil {
		return err
	}
	client.Inbounds = raw
	updates := map[string]any{"inbounds": raw}
	// A missing advertised hostname must preserve existing link observations.
	// The import owner resolves the current host setting before this command.
	if strings.TrimSpace(hostname) != "" {
		if err := UpdateLinksWithFixedInbounds(tx, []*model.Client{client}, hostname); err != nil {
			return err
		}
		updates["links"] = client.Links
	}
	return tx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(updates).Error
}
