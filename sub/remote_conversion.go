package sub

import (
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func remoteClientConversionOptions(settings *service.SettingService, target string) remotesub.ClientConversionOptions {
	groupAdaptation := ""
	rawPolicy := ""
	if settings != nil {
		if value, err := settings.GetSubRemoteGroupAdaptation(); err == nil {
			groupAdaptation = value
		}
		if value, err := settings.GetSubRemoteConversionPolicy(); err == nil {
			rawPolicy = value
		}
	}
	return remotesub.ClientConversionOptions{
		Target: target,
		Policy: subconversion.ParsePolicy(rawPolicy, groupAdaptation),
	}
}
