package apply

func SupportedObjectStrings() []string {
	objects := make([]string, 0, len(supportedObjects))
	for _, object := range supportedObjects {
		objects = append(objects, object.String())
	}
	return objects
}

func MutationHandlerObjectStrings() []string { return SupportedObjectStrings() }

func (r Router) HandlerObjectStrings() []string {
	objects := make([]string, 0, len(r.handlers))
	for object := range r.handlers {
		objects = append(objects, object.String())
	}
	return objects
}
