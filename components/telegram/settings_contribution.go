//go:build !minimal

package telegram

import (
	canonical "github.com/MalenkiySolovey/solovey-component-telegram/settings"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func registerSettingContribution() func() {
	return service.RegisterSettingContribution(id, service.SettingContribution{
		Defaults:                telegramsettings.Defaults(),
		Encrypted:               telegramsettings.EncryptedKeys(),
		ClearableEmptyEncrypted: telegramsettings.ClearableEmptyEncryptedKeys(),
		ImportAliases:           canonical.ImportAliases(),
		Fields:                  telegramSettingFields(),
		Validators: []service.SettingValidator{
			telegramsettings.Validate,
		},
	})
}

func telegramSettingFields() []settingsschema.Field {
	source := canonical.Fields()
	fields := make([]settingsschema.Field, 0, len(source))
	for _, field := range source {
		fields = append(fields, settingsschema.Field{Key: field.Key, Page: field.Page, Group: field.Group, Type: settingsschema.FieldType(field.Type), LabelKey: field.LabelKey, Order: field.Order, Advanced: field.Advanced, Options: field.Options, Min: field.Min, Max: field.Max})
	}
	return fields
}
