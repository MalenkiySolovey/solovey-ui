package steps

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func migrateClientSchema(db *gorm.DB) error {
	rows, err := db.Raw("PRAGMA table_info(clients)").Rows()
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			cname     string
			ctype     string
			notnull   int
			dfltValue interface{}
			pk        int
		)

		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if cname == "config" || cname == "inbounds" || cname == "links" {
			if ctype == "text" {
				fmt.Printf("Column %s has type TEXT\n", cname)
				oldData := make([]struct {
					Id   uint
					Data string
				}, 0)
				if err := db.Model(model.Client{}).Select("id", cname+" as data").Scan(&oldData).Error; err != nil {
					return err
				}
				for _, data := range oldData {
					var newData []byte
					switch cname {
					case "inbounds":
						inbounds := strings.Split(data.Data, ",")
						newData, err = json.MarshalIndent(inbounds, "", "  ")
					case "config":
						jsonData := map[string]interface{}{}
						if err := json.Unmarshal([]byte(data.Data), &jsonData); err != nil {
							return fmt.Errorf("decode legacy client %d config: %w", data.Id, err)
						}
						newData, err = json.MarshalIndent(jsonData, "", "  ")
					case "links":
						jsonData := make([]interface{}, 0)
						if err := json.Unmarshal([]byte(data.Data), &jsonData); err != nil {
							return fmt.Errorf("decode legacy client %d links: %w", data.Id, err)
						}
						newData, err = json.MarshalIndent(jsonData, "", "  ")
					}
					if err != nil {
						return fmt.Errorf("encode legacy client %d %s: %w", data.Id, cname, err)
					}
					err = db.Model(model.Client{}).Where("id = ?", data.Id).UpdateColumn(cname, newData).Error
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return rows.Err()
}

func deleteOldWebSecret(db *gorm.DB) error {
	return db.Exec("DELETE FROM settings WHERE key = ?", "webSecret").Error
}

func changesObj(db *gorm.DB) error {
	// A genuinely old s-ui backup can predate the `changes` table. Guard the
	// update so migrating such a backup does not hard-fail with
	// "no such table: changes" (AutoMigrate in sqlite.Init creates it afterwards).
	if !db.Migrator().HasTable("changes") {
		return nil
	}
	return db.Exec("UPDATE changes SET obj = CAST('\"' || CAST(obj AS TEXT) || '\"' AS BLOB) WHERE actor = ? and obj not like ?", "DepleteJob", "\"%\"").Error
}

func normalizeClientStorage(db *gorm.DB) error {
	err := migrateClientSchema(db)
	if err != nil {
		return err
	}
	err = deleteOldWebSecret(db)
	if err != nil {
		return err
	}
	err = changesObj(db)
	if err != nil {
		return err
	}
	return nil
}
