// Package identity owns persisted runtime entity name and tag invariants.
package identity

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const (
	maxTypeBytes = 128
	maxTagBytes  = 256
	maxNameBytes = 256
)

func ValidateTypeTag(entityType, tag string) error {
	if err := validateText("entity type", entityType, maxTypeBytes); err != nil {
		return err
	}
	return validateText("entity tag", tag, maxTagBytes)
}

func ValidateTag(tag string) error {
	return validateText("entity tag", tag, maxTagBytes)
}

func ValidateName(name string) error {
	return validateText("entity name", name, maxNameBytes)
}

// EnsureOutboundTagAvailable enforces the shared sing-box outbound namespace
// across locally persisted outbound and endpoint rows.
func EnsureOutboundTagAvailable(tx *gorm.DB, tag string, ownOutboundID, ownEndpointID uint) error {
	if tx == nil {
		return errors.New("entity persistence is unavailable")
	}
	var outboundCount int64
	query := tx.Model(&model.Outbound{}).Where("tag = ?", tag)
	if ownOutboundID != 0 {
		query = query.Where("id <> ?", ownOutboundID)
	}
	if err := query.Count(&outboundCount).Error; err != nil {
		return err
	}
	var endpointCount int64
	query = tx.Model(&model.Endpoint{}).Where("tag = ?", tag)
	if ownEndpointID != 0 {
		query = query.Where("id <> ?", ownEndpointID)
	}
	if err := query.Count(&endpointCount).Error; err != nil {
		return err
	}
	if outboundCount+endpointCount != 0 {
		return fmt.Errorf("outbound tag %q is already in use", tag)
	}
	return nil
}

// ValidateStored verifies the identity invariants of rows that can enter the
// database through migration or restore without passing through the save APIs.
func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return errors.New("entity persistence is unavailable")
	}
	type storedIdentity struct {
		ID   uint
		Type string
		Tag  string
	}
	validateTable := func(table string, shared map[string]string) error {
		if !db.Migrator().HasTable(table) {
			return nil
		}
		var rows []storedIdentity
		if err := db.Table(table).Select("id", "type", "tag").Order("id").Scan(&rows).Error; err != nil {
			return fmt.Errorf("load stored %s identities: %w", table, err)
		}
		for _, row := range rows {
			if err := ValidateTypeTag(row.Type, row.Tag); err != nil {
				return fmt.Errorf("stored %s row %d: %w", table, row.ID, err)
			}
			if previous, exists := shared[row.Tag]; exists {
				return fmt.Errorf("stored %s row %d duplicates tag %q owned by %s", table, row.ID, row.Tag, previous)
			}
			shared[row.Tag] = fmt.Sprintf("%s row %d", table, row.ID)
		}
		return nil
	}

	for _, table := range []string{"inbounds", "services"} {
		if err := validateTable(table, make(map[string]string)); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("clients") {
		type storedName struct {
			ID   uint
			Name string
		}
		var clients []storedName
		if err := db.Table("clients").Select("id", "name").Order("id").Scan(&clients).Error; err != nil {
			return fmt.Errorf("load stored client identities: %w", err)
		}
		seen := make(map[string]uint, len(clients))
		for _, client := range clients {
			if err := ValidateName(client.Name); err != nil {
				return fmt.Errorf("stored client row %d: %w", client.ID, err)
			}
			if previous, exists := seen[client.Name]; exists {
				return fmt.Errorf("stored client row %d duplicates name %q owned by client row %d", client.ID, client.Name, previous)
			}
			seen[client.Name] = client.ID
		}
	}
	outboundNamespace := make(map[string]string)
	for _, table := range []string{"outbounds", "endpoints"} {
		if err := validateTable(table, outboundNamespace); err != nil {
			return err
		}
	}
	return nil
}

func validateText(label, value string, maximum int) error {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is invalid", label)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s is invalid", label)
		}
	}
	return nil
}
