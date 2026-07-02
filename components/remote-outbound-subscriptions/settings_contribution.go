//go:build !minimal

package remoteoutboundsubscriptions

import (
	remotesettings "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/settings"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func registerSettingContribution() func() {
	return service.RegisterSettingContribution(id, service.SettingContribution{
		Defaults: remotesettings.Defaults(),
		Fields: []settingsschema.Field{
			{Key: remotesettings.GroupAdaptationKey, Page: settingsschema.PageSettings, Group: settingsschema.GroupSubscription, Type: settingsschema.FieldTypeEnum, LabelKey: "remoteOutbound.setting.groupAdaptation", Options: []string{"urltest", "selector", "failover"}, Advanced: true, Order: 220},
			{Key: remotesettings.ConversionPolicyKey, Page: settingsschema.PageSettings, Group: settingsschema.GroupSubscription, Type: settingsschema.FieldTypeJSON, LabelKey: "remoteOutbound.setting.conversionPolicy", Advanced: true, Order: 230},
		},
		Validators: []service.SettingValidator{
			validateRemoteOutboundSettingInput,
		},
	})
}

func validateRemoteOutboundSettingInput(key string, value string, _ string) error {
	switch key {
	case remotesettings.GroupAdaptationKey:
		if !validRemoteGroupAdaptation(value) {
			return common.NewError("invalid remote outbound group adaptation setting: ", key)
		}
	case remotesettings.ConversionPolicyKey:
		if err := subconversion.ValidatePolicyJSON(value); err != nil {
			return common.NewError("invalid remote outbound conversion policy setting: ", err)
		}
	}
	return nil
}

func validRemoteGroupAdaptation(value string) bool {
	switch value {
	case "urltest", "selector", "failover":
		return true
	default:
		return false
	}
}
