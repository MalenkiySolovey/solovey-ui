package importxui

import (
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func destinationServer(tx *gorm.DB) string {
	for _, key := range []string{"subDomain", "subListen", "webDomain", "webListen"} {
		var value string
		if err := tx.Model(model.Setting{}).Select("value").Where("key = ?", key).Scan(&value).Error; err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "127.0.0.1"
}
