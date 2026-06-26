package apply

type Object string

const (
	ObjectClients   Object = "clients"
	ObjectTLS       Object = "tls"
	ObjectInbounds  Object = "inbounds"
	ObjectOutbounds Object = "outbounds"
	ObjectServices  Object = "services"
	ObjectEndpoints Object = "endpoints"
	ObjectConfig    Object = "config"
	ObjectSettings  Object = "settings"
)

var supportedObjects = []Object{
	ObjectClients,
	ObjectConfig,
	ObjectEndpoints,
	ObjectInbounds,
	ObjectOutbounds,
	ObjectServices,
	ObjectSettings,
	ObjectTLS,
}

func (o Object) String() string {
	return string(o)
}

func SupportedObjects() []Object {
	return append([]Object(nil), supportedObjects...)
}

func SupportedObjectStrings() []string {
	objects := make([]string, 0, len(supportedObjects))
	for _, object := range supportedObjects {
		objects = append(objects, object.String())
	}
	return objects
}

func ParseObject(object string) (Object, bool) {
	saveObject := Object(object)
	for _, supported := range supportedObjects {
		if saveObject == supported {
			return saveObject, true
		}
	}
	return "", false
}
