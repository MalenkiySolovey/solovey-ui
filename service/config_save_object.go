package service

type configSaveObject string

const (
	configSaveObjectClients   configSaveObject = "clients"
	configSaveObjectTLS       configSaveObject = "tls"
	configSaveObjectInbounds  configSaveObject = "inbounds"
	configSaveObjectOutbounds configSaveObject = "outbounds"
	configSaveObjectServices  configSaveObject = "services"
	configSaveObjectEndpoints configSaveObject = "endpoints"
	configSaveObjectConfig    configSaveObject = "config"
	configSaveObjectSettings  configSaveObject = "settings"
)

var supportedConfigSaveObjects = []configSaveObject{
	configSaveObjectClients,
	configSaveObjectConfig,
	configSaveObjectEndpoints,
	configSaveObjectInbounds,
	configSaveObjectOutbounds,
	configSaveObjectServices,
	configSaveObjectSettings,
	configSaveObjectTLS,
}

func (o configSaveObject) String() string {
	return string(o)
}

func parseConfigSaveObject(object string) (configSaveObject, bool) {
	saveObject := configSaveObject(object)
	for _, supported := range supportedConfigSaveObjects {
		if saveObject == supported {
			return saveObject, true
		}
	}
	return "", false
}

func supportedConfigSaveObjectStrings() []string {
	objects := make([]string, 0, len(supportedConfigSaveObjects))
	for _, object := range supportedConfigSaveObjects {
		objects = append(objects, object.String())
	}
	return objects
}
