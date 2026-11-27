//go:build !minimal

package zapret

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-host/componenthost/durable"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
	"gorm.io/gorm"
)

func migrateCatalog(_ context.Context, db *gorm.DB) error {
	return catalogstore.Migrate(db, func(raw json.RawMessage) error {
		_, err := readCatalog(raw)
		return err
	})
}

func inspectCatalogDrop(_ context.Context, db *gorm.DB, _ time.Time) durable.AuthorityStatus {
	refs, err := outbounds.ZapretReferences(db)
	if err != nil {
		return durable.AuthorityStatus{State: "UNAVAILABLE", ReasonCodes: []string{"ZAPRET_BINDINGS_UNAVAILABLE"}}
	}
	if len(refs) != 0 {
		return durable.AuthorityStatus{State: "BLOCKED", ReasonCodes: []string{"ZAPRET_BINDINGS_EXIST"}}
	}
	return durable.AuthorityStatus{State: "VERIFIED_SAFE"}
}

func dropCatalog(ctx context.Context, db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if inspectCatalogDrop(ctx, tx, time.Time{}).State != "VERIFIED_SAFE" {
			return errors.New("clear Zapret outbound references before dropping the catalog")
		}
		return catalogstore.Drop(tx)
	})
}
